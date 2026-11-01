package timer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/timer"
)

// insertClosed adds a closed entry and returns its id. The row is closed with
// raw SQL because the guarded UpdateClosedEntry (the API under test) refuses
// to touch running rows by design.
func insertClosed(t *testing.T, f *fixture, projectID int64, start, end time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := store.InsertEntry(ctx, f.svc.DB.SQL, f.uid, projectID, start)
	if err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	_, err = f.svc.DB.SQL.ExecContext(ctx, `UPDATE entries SET end_at = ? WHERE id = ?`,
		store.FormatTime(end), id)
	if err != nil {
		t.Fatalf("close entry: %v", err)
	}
	return id
}

func TestRecentClosedScopedToUserNewestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Another user's entry must never appear.
	if _, err := store.InsertEntry(ctx, f.svc.DB.SQL, f.otherUser, f.otherProjs, epoch); err != nil {
		t.Fatalf("other user entry: %v", err)
	}
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	id1 := insertClosed(t, f, f.activeA, base, base.Add(time.Hour))
	id2 := insertClosed(t, f, f.activeB, base.Add(2*time.Hour), base.Add(4*time.Hour))
	if err := f.svc.Start(ctx, f.uid, f.activeA, time.Now().UTC()); err != nil {
		t.Fatalf("start running entry: %v", err)
	}

	entries, err := f.svc.RecentClosed(ctx, f.uid)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("closed entries = %d, want 2 (running excluded)", len(entries))
	}
	if entries[0].ID != id2 || entries[1].ID != id1 {
		t.Errorf("order = [%d %d], want [%d %d] newest first",
			entries[0].ID, entries[1].ID, id2, id1)
	}
	for _, e := range entries {
		if e.UserID != f.uid {
			t.Errorf("entry %d leaks another user's data", e.ID)
		}
	}
}

func TestUpdateClosedEntryEnd(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	id := insertClosed(t, f, f.activeA, base, base.Add(time.Hour))
	newEnd := base.Add(2 * time.Hour)
	if err := f.svc.Update(ctx, f.uid, id, f.activeA, base, newEnd); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EndAt == nil || !got.EndAt.Equal(newEnd) {
		t.Errorf("end_at = %v, want %v", got.EndAt, newEnd)
	}
}

func TestUpdateMovesToAnotherActiveProject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	id := insertClosed(t, f, f.activeA, base, base.Add(time.Hour))
	if err := f.svc.Update(ctx, f.uid, id, f.activeB, base, base.Add(time.Hour)); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProjectID != f.activeB {
		t.Errorf("project_id = %d, want %d", got.ProjectID, f.activeB)
	}
}

func TestUpdateRejectsRetiredOrForeignProject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	id := insertClosed(t, f, f.activeA, base, base.Add(time.Hour))

	if err := f.svc.Update(ctx, f.uid, id, f.retired, base, base.Add(time.Hour)); !errors.Is(err, timer.ErrRetired) {
		t.Errorf("move to retired err = %v, want ErrRetired", err)
	}
	if err := f.svc.Update(ctx, f.uid, id, f.otherProjs, base, base.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("move to foreign project err = %v, want ErrNotFound", err)
	}
	got, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProjectID != f.activeA {
		t.Errorf("entry changed to project %d, must stay %d", got.ProjectID, f.activeA)
	}
}

func TestUpdateRejectsEndNotAfterStart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	id := insertClosed(t, f, f.activeA, base, base.Add(time.Hour))
	if err := f.svc.Update(ctx, f.uid, id, f.activeA, base, base); !errors.Is(err, timer.ErrInvalidRange) {
		t.Errorf("equal range err = %v, want ErrInvalidRange", err)
	}
	if err := f.svc.Update(ctx, f.uid, id, f.activeA, base.Add(time.Hour), base); !errors.Is(err, timer.ErrInvalidRange) {
		t.Errorf("reversed range err = %v, want ErrInvalidRange", err)
	}
	got, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EndAt == nil || !got.EndAt.Equal(base.Add(time.Hour)) {
		t.Errorf("entry end changed to %v, must stay untouched", got.EndAt)
	}
}

func TestRunningEntryNotEditableOrDeletable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.Start(ctx, f.uid, f.activeA, time.Now().UTC()); err != nil {
		t.Fatalf("start: %v", err)
	}
	running, ok, err := f.svc.Running(ctx, f.uid)
	if err != nil || !ok {
		t.Fatalf("running: ok=%v err=%v", ok, err)
	}
	base := running.StartAt
	if err := f.svc.Update(ctx, f.uid, running.ID, f.activeB, base, base.Add(time.Hour)); !errors.Is(err, timer.ErrRunning) {
		t.Errorf("update err = %v, want ErrRunning", err)
	}
	if err := f.svc.Delete(ctx, f.uid, running.ID); !errors.Is(err, timer.ErrRunning) {
		t.Errorf("delete err = %v, want ErrRunning", err)
	}
	after, ok, err := f.svc.Running(ctx, f.uid)
	if err != nil || !ok {
		t.Fatal("running entry was disturbed by rejected update/delete")
	}
	if after.ID != running.ID || after.ProjectID != running.ProjectID {
		t.Errorf("running entry changed: %+v", after)
	}
}

func TestDeleteClosedEntryOnlyRemovesEntry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	id := insertClosed(t, f, f.activeA, base, base.Add(time.Hour))
	if err := f.svc.Delete(ctx, f.uid, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("entry still present: %v", err)
	}
	var projects, users int
	if err := f.svc.DB.SQL.QueryRow(`SELECT count(*) FROM projects WHERE user_id = ?`, f.uid).Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if err := f.svc.DB.SQL.QueryRow(`SELECT count(*) FROM users WHERE id = ?`, f.uid).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if projects != 3 || users != 1 {
		t.Errorf("projects=%d users=%d, want 3/1 (project/user data untouched)", projects, users)
	}
}
