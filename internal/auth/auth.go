package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/timetracker/internal/store"
)

// Cookie names and lifetimes shared with the web layer.
const (
	SessionCookie = "tt_session"
	CSRFCookie    = "tt_csrf"
	SessionTTL    = 30 * 24 * time.Hour
	CSRFTTL       = time.Hour
)

var (
	// ErrDuplicateUser is returned when the (case-insensitive) username exists.
	ErrDuplicateUser = errors.New("username already exists")
	// ErrInvalidInput is a registration validation failure.
	ErrInvalidInput = errors.New("invalid username or password")
	// ErrBadCredentials is returned for any failed login, without detail.
	ErrBadCredentials = errors.New("invalid username or password")
	// ErrNoSession means the token has no live session row.
	ErrNoSession = errors.New("no valid session")
)

// Service implements registration, login, and session lifecycle on top of
// store queries.
type Service struct {
	DB     *store.DB
	Hasher Hasher
}

// Register validates input, hashes the password, and inserts the user.
func (s *Service) Register(ctx context.Context, username, password string, now time.Time) (int64, error) {
	name := strings.TrimSpace(username)
	if !validUsername(name) || !validPassword(password) {
		return 0, ErrInvalidInput
	}
	passhash, err := s.Hasher.Hash(password)
	if err != nil {
		return 0, fmt.Errorf("hash password: %w", err)
	}
	id, err := store.InsertUser(ctx, s.DB.SQL, name, passhash, now)
	if store.IsConstraintViolation(err) {
		return 0, ErrDuplicateUser
	}
	if err != nil {
		return 0, fmt.Errorf("register user: %w", err)
	}
	return id, nil
}

// Login verifies credentials and creates a fresh 30-day session. The returned
// token is the cookie value; only its SHA-256 goes to the database.
func (s *Service) Login(ctx context.Context, username, password string, now time.Time) (string, time.Time, error) {
	user, err := store.GetUserByName(ctx, s.DB.SQL, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		_, _ = s.Hasher.Hash(password) // equalize work for unknown users
		return "", time.Time{}, ErrBadCredentials
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("login lookup: %w", err)
	}
	if !s.Hasher.Verify(user.Passhash, password) {
		return "", time.Time{}, ErrBadCredentials
	}
	// Opportunistic janitor: bound the sessions table even for users who
	// never return with their stale cookie.
	if err := store.DeleteExpiredSessions(ctx, s.DB.SQL, now); err != nil {
		// Housekeeping must never deny a valid login.
		slog.Error("purge expired sessions", "err", err)
	}
	return s.newSession(ctx, user.ID, now)
}

func (s *Service) newSession(ctx context.Context, userID int64, now time.Time) (string, time.Time, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(SessionTTL)
	if err := store.InsertSession(ctx, s.DB.SQL, hashToken(token), userID, expires); err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}
	return token, expires, nil
}

// Resolve maps a cookie token to its user. Expired rows are deleted
// opportunistically and treated as unauthenticated.
func (s *Service) Resolve(ctx context.Context, token string, now time.Time) (store.User, error) {
	sess, err := store.GetSession(ctx, s.DB.SQL, hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, ErrNoSession
	}
	if err != nil {
		return store.User{}, fmt.Errorf("resolve session: %w", err)
	}
	if !sess.ExpiresAt.After(now) {
		if err := store.DeleteSession(ctx, s.DB.SQL, sess.TokenHash); err != nil {
			return store.User{}, fmt.Errorf("delete expired session: %w", err)
		}
		return store.User{}, ErrNoSession
	}
	user, err := store.GetUserByID(ctx, s.DB.SQL, sess.UserID)
	if err != nil {
		return store.User{}, err
	}
	return user, nil
}

// Destroy deletes the session row behind a token (logout).
func (s *Service) Destroy(ctx context.Context, token string) error {
	return store.DeleteSession(ctx, s.DB.SQL, hashToken(token))
}

// NewCSRFToken returns a random hex token for the short-lived CSRF cookie.
func NewCSRFToken() (string, error) { return newToken() }

func validUsername(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 1 && n <= 50
}

func validPassword(password string) bool {
	n := utf8.RuneCountInString(password)
	return n >= 8 && n <= 256
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
