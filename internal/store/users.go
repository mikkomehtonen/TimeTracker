package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User is a registered account.
type User struct {
	ID        int64
	Username  string
	Passhash  string
	CreatedAt time.Time
}

// InsertUser adds a user and returns its id. Duplicate usernames (case
// insensitive) arrive as constraint violations.
func InsertUser(ctx context.Context, q queryable, username, passhash string, createdAt time.Time) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO users (username, passhash, created_at) VALUES (?, ?, ?)`,
		username, passhash, FormatTime(createdAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByName looks a user up case-insensitively (column collation).
func GetUserByName(ctx context.Context, q queryable, username string) (User, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, username, passhash, created_at FROM users WHERE username = ?`,
		username)
	return scanUser(row)
}

// GetUserByID loads a user by primary key.
func GetUserByID(ctx context.Context, q queryable, id int64) (User, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, username, passhash, created_at FROM users WHERE id = ?`,
		id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (User, error) {
	var u User
	var created string
	err := row.Scan(&u.ID, &u.Username, &u.Passhash, &created)
	if err != nil {
		return mapNoRows[User](err, "query user")
	}
	u.CreatedAt, err = ParseTime(created)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// mapNoRows translates sql.ErrNoRows into ErrNotFound.
func mapNoRows[T any](err error, op string) (T, error) {
	var zero T
	if err == nil {
		return zero, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return zero, ErrNotFound
	}
	return zero, fmt.Errorf("%s: %w", op, err)
}
