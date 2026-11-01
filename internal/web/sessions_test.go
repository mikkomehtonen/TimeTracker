package web_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"example.com/timetracker/internal/auth"
	"example.com/timetracker/internal/store"
)

func sha256Hex(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TestValidSessionInjectsUser covers the task 2 AC: a valid tt_session cookie
// gets the session user injected into the request context (the app page
// renders the username and the sign-out control).
func TestValidSessionInjectsUser(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	rec := tr.do("GET", "/", nil, j)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := tr.body(rec)
	if !strings.Contains(body, "alice") || !strings.Contains(body, "Sign out") {
		t.Errorf("page does not show the session user; body=%s", firstChunk(body))
	}
}

func TestExpiredSessionRedirectsAndDeletesRow(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	token := j[auth.SessionCookie]

	sum := sha256Hex(token)
	_, err := tr.db.SQL.ExecContext(context.Background(),
		`UPDATE sessions SET expires_at = ? WHERE token_hash = ?`,
		store.FormatTime(time.Now().UTC().Add(-time.Hour)), sum)
	if err != nil {
		t.Fatalf("expire session: %v", err)
	}

	rec := tr.do("GET", "/", nil, j)
	if rec.Code != http.StatusFound {
		t.Fatalf("GET / with expired session = %d, want 302", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
	var n int
	if err := tr.db.SQL.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("expired session rows remain = %d, want 0", n)
	}
}

func TestLogoutDeletesSessionAndClearsCookie(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	token := j[auth.SessionCookie]

	rec := tr.authedPOST("/logout", nil, j)
	if rec.Code != http.StatusFound {
		t.Fatalf("POST /logout = %d, want 302", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie && (c.MaxAge < 0 || c.Value == "") {
			cleared = true
		}
	}
	if !cleared {
		t.Error("tt_session cookie was not cleared")
	}
	if _, err := store.GetSession(context.Background(), tr.db.SQL, sha256Hex(token)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("session row after logout: %v, want gone", err)
	}
	rec = tr.do("GET", "/", nil, j)
	if rec.Code != http.StatusFound {
		t.Errorf("GET / after logout = %d, want 302", rec.Code)
	}
}
