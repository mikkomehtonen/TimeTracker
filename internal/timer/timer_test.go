package timer_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/timer"
)

var epoch = time.UnixMilli(0).UTC()

type fixture struct {
	dir        string
	svc        *timer.Service
	uid        int64
	activeA    int64
	activeB    int64
	retired    int64
	otherUser  int64
	otherProjs int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	uid, err := store.InsertUser(ctx, db.SQL, "alice", "hash", epoch)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	other, err := store.InsertUser(ctx, db.SQL, "bob", "hash", epoch)
	if err != nil {
		t.Fatalf("user2: %v", err)
	}
	f := &fixture{dir: dir, svc: &timer.Service{DB: db}, uid: uid, otherUser: other}
	f.activeA, f.activeB, f.retired, f.otherProjs = mustProjects(t, db, uid, other)
	return f
}

func mustProjects(t *testing.T, db *store.DB, uid, other int64) (a, b, retired, otherID int64) {
	t.Helper()
	ctx := context.Background()
	ids := []int64{}
	for _, spec := range []struct {
		user  int64
		name  string
		cutAt *time.Time
	}{{uid, "Alpha", nil}, {uid, "Beta", nil}, {uid, "Old", ptr(epoch)}, {other, "Bobs", nil}} {
		id, err := store.InsertProject(ctx, db.SQL, spec.user, spec.name, epoch)
		if err != nil {
			t.Fatalf("project %s: %v", spec.name, err)
		}
		if spec.cutAt != nil {
			if _, err := store.SetProjectRetired(ctx, db.SQL, spec.user, id, spec.cutAt); err != nil {
				t.Fatalf("retire %s: %v", spec.name, err)
			}
		}
		ids = append(ids, id)
	}
	return ids[0], ids[1], ids[2], ids[3]
}

func ptr[T any](v T) *T { return &v }

func runningCount(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.svc.DB.SQL.QueryRow(`SELECT count(*) FROM entries WHERE user_id = ? AND end_at IS NULL`, f.uid).Scan(&n); err != nil {
		t.Fatalf("count running: %v", err)
	}
	return n
}

func totalEntries(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.svc.DB.SQL.QueryRow(`SELECT count(*) FROM entries WHERE user_id = ?`, f.uid).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

func TestStartNoRunning(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Start(context.Background(), f.uid, f.activeA, epoch); err != nil {
		t.Fatalf("start: %v", err)
	}
	entry, ok, err := f.svc.Running(context.Background(), f.uid)
	if err != nil || !ok {
		t.Fatalf("running entry missing: ok=%v err=%v", ok, err)
	}
	if entry.ProjectID != f.activeA || entry.EndAt != nil {
		t.Errorf("entry = %+v, want project %d running", entry, f.activeA)
	}
}

func TestStartAutoSwitchUsesSameNow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	now2 := time.Date(2026, 5, 1, 11, 30, 0, 0, time.UTC)
	if err := f.svc.Start(ctx, f.uid, f.activeA, now1); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if err := f.svc.Start(ctx, f.uid, f.activeB, now2); err != nil {
		t.Fatalf("start B: %v", err)
	}
	closed, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, mustClosedID(t, f, f.activeA))
	if err != nil {
		t.Fatalf("closed entry: %v", err)
	}
	running, ok, err := f.svc.Running(ctx, f.uid)
	if err != nil || !ok {
		t.Fatalf("running after switch: ok=%v err=%v", ok, err)
	}
	if closed.EndAt == nil || !closed.EndAt.Equal(now2) {
		t.Errorf("A end_at = %v, want %v", closed.EndAt, now2)
	}
	if !running.StartAt.Equal(now2) {
		t.Errorf("B start_at = %v, want equal to A end_at %v", running.StartAt, closed.EndAt)
	}
	if n := runningCount(t, f); n != 1 {
		t.Errorf("running entries = %d, want exactly 1", n)
	}
}

func mustClosedID(t *testing.T, f *fixture, projectID int64) int64 {
	t.Helper()
	var id int64
	err := f.svc.DB.SQL.QueryRow(`SELECT id FROM entries WHERE user_id = ? AND project_id = ? AND end_at IS NOT NULL`,
		f.uid, projectID).Scan(&id)
	if err != nil {
		t.Fatalf("closed entry for project %d: %v", projectID, err)
	}
	return id
}

func TestStartSameProjectIsNoop(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if err := f.svc.Start(ctx, f.uid, f.activeA, now1); err != nil {
		t.Fatalf("start: %v", err)
	}
	later := now1.Add(time.Hour)
	if err := f.svc.Start(ctx, f.uid, f.activeA, later); err != nil {
		t.Fatalf("start again: %v", err)
	}
	running, ok, err := f.svc.Running(ctx, f.uid)
	if err != nil || !ok {
		t.Fatalf("running: ok=%v err=%v", ok, err)
	}
	if !running.StartAt.Equal(now1) {
		t.Errorf("start_at = %v, want unchanged %v", running.StartAt, now1)
	}
	if n := runningCount(t, f); n != 1 {
		t.Errorf("running entries = %d, want 1", n)
	}
}

func TestStopClosesRunning(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	now2 := now1.Add(42 * time.Minute)
	if err := f.svc.Start(ctx, f.uid, f.activeA, now1); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := f.svc.Stop(ctx, f.uid, now2); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, ok, _ := f.svc.Running(ctx, f.uid); ok {
		t.Fatal("entry still running after stop")
	}
	closed, err := store.GetEntry(ctx, f.svc.DB.SQL, f.uid, mustClosedID(t, f, f.activeA))
	if err != nil {
		t.Fatalf("closed entry: %v", err)
	}
	if closed.EndAt == nil || !closed.EndAt.Equal(now2) {
		t.Errorf("end_at = %v, want exactly the now passed to Stop (%v)", closed.EndAt, now2)
	}
}

func TestStopNoRunningIsNoop(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Stop(context.Background(), f.uid, time.Now().UTC()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if n := totalEntries(t, f); n != 0 {
		t.Errorf("entries = %d, want 0 (no-op must not insert)", n)
	}
}

func TestStartRetiredProjectLeavesTimerUntouched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if err := f.svc.Start(ctx, f.uid, f.activeA, now1); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := f.svc.Start(ctx, f.uid, f.retired, now1.Add(time.Hour)); !errors.Is(err, timer.ErrRetired) {
		t.Fatalf("start retired err = %v, want ErrRetired", err)
	}
	running, ok, err := f.svc.Running(ctx, f.uid)
	if err != nil || !ok {
		t.Fatalf("running entry was disturbed: ok=%v err=%v", ok, err)
	}
	if running.ProjectID != f.activeA {
		t.Errorf("running moved to project %d, want %d", running.ProjectID, f.activeA)
	}
	if n := totalEntries(t, f); n != 1 {
		t.Errorf("entries = %d, want 1 (no new entry)", n)
	}
}

// TestConcurrentStartsSameHandle documents that with one pooled connection
// the starts serialize and the second takes the auto-switch path; the partial
// unique index (store_test) plus the cross-handle test below cover the rest.
func TestConcurrentStartsSameHandle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	ids := []int64{f.activeA, f.activeB}
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = f.svc.Start(ctx, f.uid, ids[i], now)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(err, timer.ErrConflict) {
			t.Fatalf("start %d: %v (want nil or ErrConflict)", i, err)
		}
	}
	assertSingleRunningIntegrity(t, f)
}

// TestConcurrentStartsAcrossHandles races two Start calls that each hold
// their own *sql.DB against the same file, so the partial unique index and
// ErrConflict are enforced across real concurrent connections. Retryable
// outcomes (nil or ErrConflict from the index, SQLITE_BUSY from the write
// upgrade race) are all acceptable; the running-entry invariant is not.
func TestConcurrentStartsAcrossHandles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	db2, err := store.Open(f.dir)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	svcs := []*timer.Service{f.svc, {DB: db2}}
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	ids := []int64{f.activeA, f.activeB}
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = svcs[i].Start(ctx, f.uid, ids[i], now)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(err, timer.ErrConflict) && !store.IsBusy(err) {
			t.Fatalf("start %d: %v (want nil, ErrConflict, or SQLITE_BUSY)", i, err)
		}
	}
	assertSingleRunningIntegrity(t, f)
}

func assertSingleRunningIntegrity(t *testing.T, f *fixture) {
	if n := runningCount(t, f); n != 1 {
		t.Errorf("running entries = %d, want exactly 1", n)
	}
	if n := totalEntries(t, f); n < 1 || n > 2 {
		t.Errorf("total entries = %d, want 1 or 2 (state not corrupted)", n)
	}
	var openClosed int
	err := f.svc.DB.SQL.QueryRow(`SELECT count(*) FROM entries WHERE user_id = ? AND end_at IS NOT NULL`, f.uid).Scan(&openClosed)
	if err != nil {
		t.Fatalf("count closed: %v", err)
	}
	if totalEntries(t, f)-openClosed != 1 {
		t.Error("inconsistent closed/running split after concurrency test")
	}
}
