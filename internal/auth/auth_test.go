package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"example.com/timetracker/internal/store"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Service{DB: db, Hasher: Hasher{Iterations: 1000}}
}

func countRows(t *testing.T, s *Service, table string) int {
	t.Helper()
	var n int
	if err := s.DB.SQL.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestRegisterStoresPBKDF2HashNoPlaintext(t *testing.T) {
	s := newService(t)
	const pw = "password1234" // 12 characters
	if _, err := s.Register(context.Background(), "alice", pw, time.UnixMilli(0).UTC()); err != nil {
		t.Fatalf("register: %v", err)
	}
	u, err := store.GetUserByName(context.Background(), s.DB.SQL, "alice")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !regexp.MustCompile(`^pbkdf2\$sha256\$\d+\$`).MatchString(u.Passhash) {
		t.Errorf("passhash %q does not start with pbkdf2$sha256$<iter>", u.Passhash)
	}
	if !s.Hasher.Verify(u.Passhash, pw) {
		t.Error("stored passhash does not verify the password")
	}
	for _, col := range []string{u.Passhash, u.Username, store.FormatTime(u.CreatedAt)} {
		if strings.Contains(col, pw) {
			t.Errorf("column %q leaks the plaintext password", col)
		}
	}
}

func TestRegisterDuplicateIsCaseInsensitive(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, "Alice", "password1234", time.UnixMilli(0).UTC()); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := s.Register(ctx, "alice", "password5678", time.UnixMilli(0).UTC()); !errors.Is(err, ErrDuplicateUser) {
		t.Errorf("second register err = %v, want ErrDuplicateUser", err)
	}
	if n := countRows(t, s, "users"); n != 1 {
		t.Errorf("user rows = %d, want 1", n)
	}
}

func TestRegisterValidationRejectsBadInput(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, "  ", "password1234", time.UnixMilli(0).UTC()); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("blank username err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.Register(ctx, "bob", "short", time.UnixMilli(0).UTC()); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("7-char password err = %v, want ErrInvalidInput", err)
	}
	if n := countRows(t, s, "users"); n != 0 {
		t.Errorf("user rows = %d after rejected registrations, want 0", n)
	}
}

func TestLoginSuccessCreates30DaySession(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := s.Register(ctx, "alice", "password1234", now); err != nil {
		t.Fatalf("register: %v", err)
	}
	token, expires, err := s.Login(ctx, "alice", "password1234", now)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(token) {
		t.Errorf("token %q is not 32 bytes of hex", token)
	}
	want := now.Add(SessionTTL)
	if !expires.Equal(want) {
		t.Errorf("expires = %s, want %s (30 days ahead)", expires, want)
	}
	sess, err := store.GetSession(ctx, s.DB.SQL, hashToken(token))
	if err != nil {
		t.Fatalf("session row missing: %v", err)
	}
	if !sess.ExpiresAt.Equal(want) {
		t.Errorf("stored expires = %s, want %s", sess.ExpiresAt, want)
	}
}

func TestLoginWrongPasswordCreatesNoSession(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, "alice", "password1234", time.UnixMilli(0).UTC()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, _, err := s.Login(ctx, "alice", "wrongpassword", time.Now().UTC()); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("login err = %v, want ErrBadCredentials", err)
	}
	if n := countRows(t, s, "sessions"); n != 0 {
		t.Errorf("session rows = %d, want 0", n)
	}
}

func TestResolveValidSession(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	uid, err := s.Register(ctx, "alice", "password1234", now)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token, _, err := s.Login(ctx, "alice", "password1234", now)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	user, err := s.Resolve(ctx, token, now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if user.ID != uid || user.Username != "alice" {
		t.Errorf("resolved user = %+v, want id %d alice", user, uid)
	}
}

func TestResolveExpiredSessionDeletesRow(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	uid, err := s.Register(ctx, "alice", "password1234", time.UnixMilli(0).UTC())
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token, err := newToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if err := store.InsertSession(ctx, s.DB.SQL, hashToken(token), uid, past); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.Resolve(ctx, token, time.Now().UTC()); !errors.Is(err, ErrNoSession) {
		t.Errorf("resolve err = %v, want ErrNoSession", err)
	}
	if n := countRows(t, s, "sessions"); n != 0 {
		t.Errorf("expired session rows remain: %d", n)
	}
}

func TestDestroyRemovesSession(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, "alice", "password1234", time.UnixMilli(0).UTC()); err != nil {
		t.Fatalf("register: %v", err)
	}
	token, _, err := s.Login(ctx, "alice", "password1234", time.Now().UTC())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := s.Destroy(ctx, token); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := store.GetSession(ctx, s.DB.SQL, hashToken(token)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("session after destroy: %v, want ErrNotFound", err)
	}
}

// TestLoginPurgesExpiredSessions verifies the login-time janitor bounds the
// sessions table even for users who never come back with their stale cookie.
func TestLoginPurgesExpiredSessions(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := s.Register(ctx, "alice", "password1234", now); err != nil {
		t.Fatalf("register: %v", err)
	}
	bobID, err := s.Register(ctx, "bob", "password1234", now)
	if err != nil {
		t.Fatalf("register2: %v", err)
	}
	dead, err := newToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	live, err := newToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if err := store.InsertSession(ctx, s.DB.SQL, hashToken(dead), bobID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("insert stale session: %v", err)
	}
	if err := store.InsertSession(ctx, s.DB.SQL, hashToken(live), bobID, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert live session: %v", err)
	}
	token, _, err := s.Login(ctx, "alice", "password1234", now)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := store.GetSession(ctx, s.DB.SQL, hashToken(dead)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("stale session after login janitor: %v, want gone", err)
	}
	// The janitor is a table-wide DELETE on the login hot path: live rows of
	// other users must survive it, or it logs everyone out.
	if _, err := store.GetSession(ctx, s.DB.SQL, hashToken(live)); err != nil {
		t.Errorf("live session purged: %v", err)
	}
	if _, err := store.GetSession(ctx, s.DB.SQL, hashToken(token)); err != nil {
		t.Errorf("fresh login session does not resolve: %v", err)
	}
}
