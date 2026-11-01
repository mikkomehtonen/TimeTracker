package store

import (
	"context"
	"database/sql"
	"time"
)

// Project is a per-user project; RetiredAt nil means active.
type Project struct {
	ID        int64
	UserID    int64
	Name      string
	RetiredAt *time.Time
	CreatedAt time.Time
}

// InsertProject adds a project and returns its id. Duplicate names per user
// arrive as constraint violations (case insensitive).
func InsertProject(ctx context.Context, q queryable, userID int64, name string, createdAt time.Time) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO projects (user_id, name, created_at) VALUES (?, ?, ?)`,
		userID, name, FormatTime(createdAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetProject loads a project only when it belongs to userID.
func GetProject(ctx context.Context, q queryable, userID, id int64) (Project, error) {
	row := q.QueryRowContext(ctx, projectSelect+` WHERE id = ? AND user_id = ?`, id, userID)
	return scanProject(row)
}

// ListProjects returns all of the user's projects, active first, by name.
func ListProjects(ctx context.Context, q queryable, userID int64) ([]Project, error) {
	rows, err := q.QueryContext(ctx,
		projectSelect+` WHERE user_id = ? ORDER BY retired_at IS NOT NULL, LOWER(name)`,
		userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenameProject updates the name of the user's own project.
func RenameProject(ctx context.Context, q queryable, userID, id int64, name string) (int64, error) {
	res, err := q.ExecContext(ctx,
		`UPDATE projects SET name = ? WHERE id = ? AND user_id = ?`,
		name, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetProjectRetired sets or clears retired_at for the user's own project.
func SetProjectRetired(ctx context.Context, q queryable, userID, id int64, retiredAt *time.Time) (int64, error) {
	var arg any
	if retiredAt != nil {
		arg = FormatTime(*retiredAt)
	}
	res, err := q.ExecContext(ctx,
		`UPDATE projects SET retired_at = ? WHERE id = ? AND user_id = ?`,
		arg, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const projectSelect = `SELECT id, user_id, name, retired_at, created_at FROM projects`

func scanProject(q scanner) (Project, error) {
	var p Project
	var retired sql.NullString
	var created string
	if err := q.Scan(&p.ID, &p.UserID, &p.Name, &retired, &created); err != nil {
		return mapNoRows[Project](err, "query project")
	}
	var err error
	if p.RetiredAt, err = parseOptionalTime(retired); err != nil {
		return Project{}, err
	}
	p.CreatedAt, err = ParseTime(created)
	if err != nil {
		return Project{}, err
	}
	return p, nil
}
