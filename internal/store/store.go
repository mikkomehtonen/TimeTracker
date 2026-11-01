package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound means a row scoped to the requesting user does not exist.
// Cross-user requests must surface as this so existence never leaks.
var ErrNotFound = errors.New("not found")

// tsLayout stores UTC timestamps with a fixed 9-digit fraction so that
// lexicographic order equals chronological order. Do not use time.RFC3339Nano,
// which trims trailing zeros.
const tsLayout = "2006-01-02T15:04:05.000000000Z07:00"

// FormatTime renders t in the canonical UTC storage format.
func FormatTime(t time.Time) string { return t.UTC().Format(tsLayout) }

// ParseTime parses a timestamp produced by FormatTime.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(tsLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}

// queryable lets one query function run on the pool or inside a transaction.
type queryable interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner is satisfied by both *sql.Row and *sql.Rows so a single scan
// function serves single-row and multi-row queries.
type scanner interface {
	Scan(dest ...any) error
}

// DB wraps the SQLite connection pool.
type DB struct {
	SQL *sql.DB
}

// Open creates dir when missing, opens timetracker.db inside it, and runs the
// idempotent migrations.
func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := filepath.Join(dir, "timetracker.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	db := &DB{SQL: sqlDB}
	if err := db.SQL.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := db.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the pool.
func (db *DB) Close() error { return db.SQL.Close() }

// Transact runs fn in a transaction, committing on success and rolling back on
// any error.
func (db *DB) Transact(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return fmt.Errorf("rollback after %w: %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// parseOptionalTime converts a nullable TEXT column into a *time.Time.
func parseOptionalTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := ParseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
