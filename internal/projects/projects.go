package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/timetracker/internal/store"
)

var (
	// ErrDuplicate is returned when the user already has this project name
	// (case-insensitive, including retired projects).
	ErrDuplicate = errors.New("project with this name already exists")
	// ErrInvalidName is the 1-100 character rule failure.
	ErrInvalidName = errors.New("project name must be 1-100 characters")
)

// Service owns project domain rules on top of store queries.
type Service struct {
	DB *store.DB
}

// normalizeName applies the single name predicate: trim, then 1-100 runes.
// Uniqueness is enforced by the store's UNIQUE (user_id, name COLLATE NOCASE).
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ErrInvalidName
	}
	return name, nil
}

// Create inserts a new active project for the user.
func (s *Service) Create(ctx context.Context, userID int64, raw string, now time.Time) (store.Project, error) {
	name, err := normalizeName(raw)
	if err != nil {
		return store.Project{}, err
	}
	id, err := store.InsertProject(ctx, s.DB.SQL, userID, name, now)
	if store.IsConstraintViolation(err) {
		return store.Project{}, ErrDuplicate
	}
	if err != nil {
		return store.Project{}, fmt.Errorf("create project: %w", err)
	}
	return store.GetProject(ctx, s.DB.SQL, userID, id)
}

// Rename re-runs the name predicate excluding the project itself. Allowed on
// active and retired projects alike.
func (s *Service) Rename(ctx context.Context, userID, id int64, raw string) (store.Project, error) {
	name, err := normalizeName(raw)
	if err != nil {
		return store.Project{}, err
	}
	if _, err := store.GetProject(ctx, s.DB.SQL, userID, id); err != nil {
		return store.Project{}, err
	}
	rows, err := store.RenameProject(ctx, s.DB.SQL, userID, id, name)
	if store.IsConstraintViolation(err) {
		return store.Project{}, ErrDuplicate
	}
	if err != nil {
		return store.Project{}, fmt.Errorf("rename project: %w", err)
	}
	if rows == 0 {
		return store.Project{}, store.ErrNotFound
	}
	return store.GetProject(ctx, s.DB.SQL, userID, id)
}

// Retire marks an active project retired; its name stays reserved.
func (s *Service) Retire(ctx context.Context, userID, id int64, now time.Time) error {
	return s.setRetired(ctx, userID, id, &now)
}

// Unretire returns a retired project to the active list.
func (s *Service) Unretire(ctx context.Context, userID, id int64) error {
	return s.setRetired(ctx, userID, id, nil)
}

func (s *Service) setRetired(ctx context.Context, userID, id int64, retiredAt *time.Time) error {
	if _, err := store.GetProject(ctx, s.DB.SQL, userID, id); err != nil {
		return err
	}
	rows, err := store.SetProjectRetired(ctx, s.DB.SQL, userID, id, retiredAt)
	if err != nil {
		return fmt.Errorf("set project retired: %w", err)
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// List partitions the user's projects into active and retired.
func (s *Service) List(ctx context.Context, userID int64) (active, retired []store.Project, err error) {
	all, err := store.ListProjects(ctx, s.DB.SQL, userID)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range all {
		if p.RetiredAt != nil {
			retired = append(retired, p)
		} else {
			active = append(active, p)
		}
	}
	return active, retired, nil
}
