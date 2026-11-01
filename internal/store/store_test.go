package store_test

import (
	"context"
	"database/sql"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"example.com/timetracker/internal/store"
)

var projSeq atomic.Int64

// nextProjectName yields collision-free names without relying on clock
// precision.
func nextProjectName() string {
	return "p" + strconv.FormatInt(projSeq.Add(1), 10)
}

func mustOpen(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustUser(t *testing.T, db *store.DB, name string) int64 {
	t.Helper()
	id, err := store.InsertUser(context.Background(), db.SQL, name, "hash", time.UnixMilli(0).UTC())
	if err != nil {
		t.Fatalf("insert user %s: %v", name, err)
	}
	return id
}

func schemaNames(t *testing.T, db *store.DB) map[string]bool {
	t.Helper()
	rows, err := db.SQL.Query(`SELECT name FROM sqlite_master WHERE type IN ('table', 'index')`)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	defer func() { _ = rows.Close() }()
	names := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan schema name: %v", err)
		}
		names[n] = true
	}
	return names
}

func TestOpenCreatesAllTablesAndIndex(t *testing.T) {
	db := mustOpen(t)
	want := []string{"users", "projects", "entries", "sessions", "one_running_per_user"}
	got := schemaNames(t, db)
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing schema object %q", name)
		}
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	dir := t.TempDir()
	db1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	uid := mustUser(t, db1, "carol")
	_ = db1.Close()

	db2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = db2.Close() }()

	if _, err := store.GetUserByID(context.Background(), db2.SQL, uid); err != nil {
		t.Fatalf("existing user lost after re-migration: %v", err)
	}
	var n int
	if err := db2.SQL.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Errorf("duplicate data after re-migration: users = %d", n)
	}
}

func insertRunning(t *testing.T, db *store.DB, userID, projectID int64) error {
	t.Helper()
	return db.Transact(context.Background(), func(tx *sql.Tx) error {
		_, err := store.InsertEntry(context.Background(), tx, userID, projectID, time.UnixMilli(1000).UTC())
		return err
	})
}

func mustProject(t *testing.T, db *store.DB, userID int64) int64 {
	t.Helper()
	id, err := store.InsertProject(context.Background(), db.SQL, userID, nextProjectName(), time.UnixMilli(0).UTC())
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id
}

func TestPartialUniqueIndexBlocksSecondRunningEntry(t *testing.T) {
	db := mustOpen(t)
	uid := mustUser(t, db, "alice")
	p1 := mustProject(t, db, uid)
	p2 := mustProject(t, db, uid)

	if err := insertRunning(t, db, uid, p1); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	second := insertRunning(t, db, uid, p2)
	if second == nil {
		t.Fatal("second running entry inserted; expected constraint error")
	}
	if !store.IsConstraintViolation(second) {
		t.Errorf("expected SQLite constraint violation, got %v", second)
	}

	first, err := store.GetRunningEntry(context.Background(), db.SQL, uid)
	if err != nil {
		t.Fatalf("first entry was disturbed: %v", err)
	}
	if first.ProjectID != p1 {
		t.Errorf("first entry project = %d, want %d", first.ProjectID, p1)
	}
}

func TestPartialUniqueIndexAllowsOneRunningPerUser(t *testing.T) {
	db := mustOpen(t)
	u1 := mustUser(t, db, "alice")
	u2 := mustUser(t, db, "bob")
	if err := insertRunning(t, db, u1, mustProject(t, db, u1)); err != nil {
		t.Fatalf("user 1 running insert: %v", err)
	}
	if err := insertRunning(t, db, u2, mustProject(t, db, u2)); err != nil {
		t.Fatalf("user 2 running insert: %v", err)
	}
	var n int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM entries WHERE end_at IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("running entries = %d, want 2", n)
	}
}

// TestIsConstraintViolationRejectsNonUniqueCodes pins the precise mapping:
// only UNIQUE/PRIMARY KEY read as "already exists"; NOT NULL and FOREIGN KEY
// failures must not be mislabeled by callers.
func TestIsConstraintViolationRejectsNonUniqueCodes(t *testing.T) {
	db := mustOpen(t)
	ctx := context.Background()

	_, err := store.InsertEntry(ctx, db.SQL, 4242, 4242, time.UnixMilli(0).UTC())
	if err == nil {
		t.Fatal("FOREIGN KEY violation expected with foreign_keys(1)")
	}
	if store.IsConstraintViolation(err) {
		t.Errorf("FK violation reported as unique constraint: %v", err)
	}

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO users (username, passhash, created_at) VALUES ('nullpass', NULL, ?)`,
		store.FormatTime(time.UnixMilli(0).UTC()))
	if err == nil {
		t.Fatal("NOT NULL violation expected")
	}
	if store.IsConstraintViolation(err) {
		t.Errorf("NOT NULL violation reported as unique constraint: %v", err)
	}
}

// TestClosedEntryStatementsSkipRunningRows pins the SQL guard the story calls
// the real safety net: the UPDATE/DELETE statements carry
// `AND end_at IS NOT NULL`, so a running row is never touched (0 rows).
func TestClosedEntryStatementsSkipRunningRows(t *testing.T) {
	db := mustOpen(t)
	ctx := context.Background()
	uid := mustUser(t, db, "alice")
	pid := mustProject(t, db, uid)
	if err := insertRunning(t, db, uid, pid); err != nil {
		t.Fatalf("insert running: %v", err)
	}
	var runID int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT id FROM entries WHERE user_id = ?`, uid).Scan(&runID); err != nil {
		t.Fatalf("running id: %v", err)
	}

	rows, err := store.UpdateClosedEntry(ctx, db.SQL, uid, runID, pid,
		time.UnixMilli(0).UTC(), time.Unix(3600, 0).UTC())
	if err != nil {
		t.Fatalf("update running row: %v", err)
	}
	if rows != 0 {
		t.Errorf("UpdateClosedEntry touched %d rows on a running entry", rows)
	}
	rows, err = store.DeleteClosedEntry(ctx, db.SQL, uid, runID)
	if err != nil {
		t.Fatalf("delete running row: %v", err)
	}
	if rows != 0 {
		t.Errorf("DeleteClosedEntry touched %d rows on a running entry", rows)
	}
	var running int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM entries WHERE user_id = ? AND end_at IS NULL`, uid).Scan(&running); err != nil {
		t.Fatalf("count: %v", err)
	}
	if running != 1 {
		t.Errorf("running entry count = %d, want 1 (untouched)", running)
	}
}
