package store

import (
	"context"
	"database/sql"
	"time"
)

// Entry is a tracked time span; EndAt nil means the entry is running.
type Entry struct {
	ID        int64
	UserID    int64
	ProjectID int64
	StartAt   time.Time
	EndAt     *time.Time
}

// InsertEntry adds an entry. The partial unique index rejects a second
// running entry for the same user.
func InsertEntry(ctx context.Context, q queryable, userID, projectID int64, startAt time.Time) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO entries (user_id, project_id, start_at) VALUES (?, ?, ?)`,
		userID, projectID, FormatTime(startAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetRunningEntry returns the user's running entry, or ErrNotFound.
func GetRunningEntry(ctx context.Context, q queryable, userID int64) (Entry, error) {
	row := q.QueryRowContext(ctx,
		entrySelect+` WHERE user_id = ? AND end_at IS NULL`, userID)
	return scanEntry(row)
}

// GetEntry returns the user's entry by id, or ErrNotFound.
func GetEntry(ctx context.Context, q queryable, userID, id int64) (Entry, error) {
	row := q.QueryRowContext(ctx,
		entrySelect+` WHERE id = ? AND user_id = ?`, id, userID)
	return scanEntry(row)
}

// ListClosedEntries returns the newest limit closed entries for the user.
func ListClosedEntries(ctx context.Context, q queryable, userID int64, limit int) ([]Entry, error) {
	rows, err := q.QueryContext(ctx,
		entrySelect+` WHERE user_id = ? AND end_at IS NOT NULL ORDER BY start_at DESC LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CloseRunningEntry stamps end_at on the user's running entry.
// The affected-row count is 0 when nothing was running.
func CloseRunningEntry(ctx context.Context, q queryable, userID int64, endAt time.Time) (int64, error) {
	res, err := q.ExecContext(ctx,
		`UPDATE entries SET end_at = ? WHERE user_id = ? AND end_at IS NULL`,
		FormatTime(endAt), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateClosedEntry rewrites a closed entry; running rows are excluded by the
// WHERE clause so running entries are never touched.
func UpdateClosedEntry(ctx context.Context, q queryable, userID, id, projectID int64, startAt, endAt time.Time) (int64, error) {
	res, err := q.ExecContext(ctx,
		`UPDATE entries SET project_id = ?, start_at = ?, end_at = ?
		  WHERE id = ? AND user_id = ? AND end_at IS NOT NULL`,
		projectID, FormatTime(startAt), FormatTime(endAt), id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteClosedEntry removes a closed entry; running rows are excluded.
func DeleteClosedEntry(ctx context.Context, q queryable, userID, id int64) (int64, error) {
	res, err := q.ExecContext(ctx,
		`DELETE FROM entries WHERE id = ? AND user_id = ? AND end_at IS NOT NULL`,
		id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const entrySelect = `SELECT id, user_id, project_id, start_at, end_at FROM entries`

func scanEntry(q scanner) (Entry, error) {
	var e Entry
	var start string
	var end sql.NullString
	if err := q.Scan(&e.ID, &e.UserID, &e.ProjectID, &start, &end); err != nil {
		return mapNoRows[Entry](err, "query entry")
	}
	var err error
	if e.EndAt, err = parseOptionalTime(end); err != nil {
		return Entry{}, err
	}
	e.StartAt, err = ParseTime(start)
	if err != nil {
		return Entry{}, err
	}
	return e, nil
}
