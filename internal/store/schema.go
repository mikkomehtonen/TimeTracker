package store

import (
	"context"
	"fmt"
)

// schemaStatements run at every startup and are idempotent.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  passhash TEXT NOT NULL,
  created_at TEXT NOT NULL
)`,
	// NOCASE uniqueness below is ASCII-only; non-ASCII look-alike duplicates
	// are intentionally permitted.
	`CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  retired_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (user_id, name COLLATE NOCASE)
)`,
	`CREATE TABLE IF NOT EXISTS entries (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  project_id INTEGER NOT NULL REFERENCES projects(id),
  start_at TEXT NOT NULL,
  end_at TEXT
)`,
	`CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  expires_at TEXT NOT NULL
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS one_running_per_user
  ON entries (user_id) WHERE end_at IS NULL`,
}

// migrate executes every schema statement; safe to run repeatedly.
func (db *DB) migrate(ctx context.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := db.SQL.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
