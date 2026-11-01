package store

import (
	"context"
	"time"
)

// Session is a server-side login session keyed by the SHA-256 of the cookie
// token.
type Session struct {
	TokenHash string
	UserID    int64
	ExpiresAt time.Time
}

// InsertSession stores a new session row.
func InsertSession(ctx context.Context, q queryable, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash, userID, FormatTime(expiresAt))
	return err
}

// DeleteExpiredSessions removes sessions whose expires_at is at or before
// now, so rows for users who never return do not accumulate.
func DeleteExpiredSessions(ctx context.Context, q queryable, now time.Time) error {
	_, err := q.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, FormatTime(now))
	return err
}

// GetSession loads a session by token hash; ErrNotFound when absent.
func GetSession(ctx context.Context, q queryable, tokenHash string) (Session, error) {
	row := q.QueryRowContext(ctx,
		`SELECT token_hash, user_id, expires_at FROM sessions WHERE token_hash = ?`,
		tokenHash)
	var s Session
	var expires string
	err := row.Scan(&s.TokenHash, &s.UserID, &expires)
	if err != nil {
		return mapNoRows[Session](err, "query session")
	}
	s.ExpiresAt, err = ParseTime(expires)
	if err != nil {
		return Session{}, err
	}
	return s, nil
}

// DeleteSession removes a session row; absent rows are not an error.
func DeleteSession(ctx context.Context, q queryable, tokenHash string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}
