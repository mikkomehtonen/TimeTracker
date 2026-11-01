package projects_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/timetracker/internal/projects"
	"example.com/timetracker/internal/store"
)

var epoch = time.UnixMilli(0).UTC()

func newService(t *testing.T) *projects.Service {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &projects.Service{DB: db}
}

func mustUser(t *testing.T, s *projects.Service, name string) int64 {
	t.Helper()
	id, err := store.InsertUser(context.Background(), s.DB.SQL, name, "hash", epoch)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func countProjects(t *testing.T, s *projects.Service) int {
	t.Helper()
	var n int
	if err := s.DB.SQL.QueryRow(`SELECT count(*) FROM projects`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestCreateActiveProject(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	p, err := s.Create(context.Background(), uid, "Website", epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.RetiredAt != nil {
		t.Errorf("new project retired_at = %v, want NULL", p.RetiredAt)
	}
}

func TestCreateDuplicateIsCaseAndSpaceInsensitive(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	ctx := context.Background()
	if _, err := s.Create(ctx, uid, "Website", epoch); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := s.Create(ctx, uid, "website ", epoch)
	if !errors.Is(err, projects.ErrDuplicate) {
		t.Errorf("err = %v, want ErrDuplicate", err)
	}
	if n := countProjects(t, s); n != 1 {
		t.Errorf("project rows = %d, want exactly 1", n)
	}
}

func TestCreateTrimsName(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	p, err := s.Create(context.Background(), uid, "  Work  ", epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Name != "Work" {
		t.Errorf("stored name = %q, want %q", p.Name, "Work")
	}
}

func TestCreateRejectsEmptyOrTooLong(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	ctx := context.Background()
	for _, raw := range []string{"", strings.Repeat("x", 101)} {
		if _, err := s.Create(ctx, uid, raw, epoch); !errors.Is(err, projects.ErrInvalidName) {
			t.Errorf("create(%q) err = %v, want ErrInvalidName", raw, err)
		}
	}
	if n := countProjects(t, s); n != 0 {
		t.Errorf("project rows = %d, want 0", n)
	}
}

func TestRenameKeepsTrackingData(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	ctx := context.Background()
	p, err := s.Create(ctx, uid, "Website", epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.InsertEntry(ctx, s.DB.SQL, uid, p.ID, epoch); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	renamed, err := s.Rename(ctx, uid, p.ID, "New site")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "New site" {
		t.Errorf("name = %q, want New site", renamed.Name)
	}
	var entries int
	if err := s.DB.SQL.QueryRow(`SELECT count(*) FROM entries WHERE project_id = ?`, p.ID).Scan(&entries); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entries != 1 {
		t.Errorf("entries for project = %d, want 1 (tracking data lost)", entries)
	}
}

func TestRenameRetiredProjectSucceeds(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	ctx := context.Background()
	p, err := s.Create(ctx, uid, "Old", epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Retire(ctx, uid, p.ID, epoch); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if _, err := s.Rename(ctx, uid, p.ID, "Older"); err != nil {
		t.Errorf("rename retired project: %v", err)
	}
}

func TestRenameCollisionIsDuplicate(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	ctx := context.Background()
	if _, err := s.Create(ctx, uid, "A", epoch); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := s.Create(ctx, uid, "B", epoch)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	if _, err := s.Rename(ctx, uid, b.ID, "a"); !errors.Is(err, projects.ErrDuplicate) {
		t.Errorf("rename err = %v, want ErrDuplicate", err)
	}
}

func TestRetireAndUnretire(t *testing.T) {
	s := newService(t)
	uid := mustUser(t, s, "alice")
	ctx := context.Background()
	p, err := s.Create(ctx, uid, "Website", epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Retire(ctx, uid, p.ID, time.UnixMilli(5000).UTC()); err != nil {
		t.Fatalf("retire: %v", err)
	}
	active, retired, err := s.List(ctx, uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(active) != 0 || len(retired) != 1 || retired[0].RetiredAt == nil {
		t.Errorf("after retire active=%d retired=%d, want 0/1 with retired_at set", len(active), len(retired))
	}
	if err := s.Unretire(ctx, uid, p.ID); err != nil {
		t.Fatalf("unretire: %v", err)
	}
	active, retired, err = s.List(ctx, uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(active) != 1 || active[0].RetiredAt != nil || len(retired) != 0 {
		t.Errorf("after un-retire active=%d retired=%d, want 1/0 with NULL retired_at", len(active), len(retired))
	}
}

func TestNoCrossUserAccess(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	uidA := mustUser(t, s, "alice")
	uidB := mustUser(t, s, "bob")
	p, err := s.Create(ctx, uidA, "Secret", epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.GetProject(ctx, s.DB.SQL, uidB, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("bob read err = %v, want ErrNotFound", err)
	}
	if _, err := s.Rename(ctx, uidB, p.ID, "Hijacked"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("bob rename err = %v, want ErrNotFound", err)
	}
	if err := s.Retire(ctx, uidB, p.ID, epoch); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("bob retire err = %v, want ErrNotFound", err)
	}
	if err := s.Unretire(ctx, uidB, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("bob un-retire err = %v, want ErrNotFound", err)
	}
}
