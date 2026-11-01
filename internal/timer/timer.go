package timer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"example.com/timetracker/internal/store"
)

var (
	// ErrRetired means the target project is retired and cannot be tracked
	// into.
	ErrRetired = errors.New("project is retired")
	// ErrConflict is a retryable race: another command changed the running
	// entry concurrently. State is never corrupted.
	ErrConflict = errors.New("timer state changed, please retry")
	// ErrRunning means the entry is still running and therefore not editable
	// or deletable.
	ErrRunning = errors.New("entry is still running")
	// ErrInvalidRange means start_at is not strictly before end_at.
	ErrInvalidRange = errors.New("entry must start before it ends")
)

// recentLimit is the fixed list size for v1 (no pagination).
const recentLimit = 100

// Service implements the start/stop/auto-switch commands and entry upkeep.
// At most one running entry per user is enforced by domain checks plus the
// one_running_per_user partial unique index as last line of defense.
type Service struct {
	DB *store.DB
}

// Start begins tracking projectID for the user in one transaction.
// Same-project start is a no-op; a different project auto-switches, closing
// the running entry with exactly the same now value the new entry starts at.
func (s *Service) Start(ctx context.Context, userID, projectID int64, now time.Time) error {
	return s.DB.Transact(ctx, func(tx *sql.Tx) error {
		return s.startTx(ctx, tx, userID, projectID, now)
	})
}

func (s *Service) startTx(ctx context.Context, tx *sql.Tx, userID, projectID int64, now time.Time) error {
	proj, err := store.GetProject(ctx, tx, userID, projectID)
	if err != nil {
		return err // other users' projects are indistinguishable: ErrNotFound
	}
	if proj.RetiredAt != nil {
		return ErrRetired
	}
	running, err := store.GetRunningEntry(ctx, tx, userID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s.insertRunning(ctx, tx, userID, projectID, now)
	case err != nil:
		return err
	case running.ProjectID == projectID:
		return nil
	default:
		if err := s.closeRunning(ctx, tx, userID, now); err != nil {
			return err
		}
		return s.insertRunning(ctx, tx, userID, projectID, now)
	}
}

func (s *Service) insertRunning(ctx context.Context, tx *sql.Tx, userID, projectID int64, now time.Time) error {
	_, err := store.InsertEntry(ctx, tx, userID, projectID, now)
	if store.IsConstraintViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}
	return nil
}

func (s *Service) closeRunning(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) error {
	rows, err := store.CloseRunningEntry(ctx, tx, userID, now)
	if err != nil {
		return fmt.Errorf("close running entry: %w", err)
	}
	if rows == 0 {
		return ErrConflict
	}
	return nil
}

// Stop closes the running entry; with nothing running it is a successful
// no-op.
func (s *Service) Stop(ctx context.Context, userID int64, now time.Time) error {
	if _, err := store.CloseRunningEntry(ctx, s.DB.SQL, userID, now); err != nil {
		return fmt.Errorf("stop timer: %w", err)
	}
	return nil
}

// Running returns the user's running entry when present.
func (s *Service) Running(ctx context.Context, userID int64) (store.Entry, bool, error) {
	entry, err := store.GetRunningEntry(ctx, s.DB.SQL, userID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Entry{}, false, nil
	}
	if err != nil {
		return store.Entry{}, false, fmt.Errorf("query running entry: %w", err)
	}
	return entry, true, nil
}

// RecentClosed returns the 100 newest closed entries, newest first.
func (s *Service) RecentClosed(ctx context.Context, userID int64) ([]store.Entry, error) {
	return store.ListClosedEntries(ctx, s.DB.SQL, userID, recentLimit)
}

// closedEntry loads an entry and guards that it is closed. Safety against a
// concurrent Stop comes from the end_at IS NOT NULL guards in the write
// statements below, not from this prior read.
func closedEntry(ctx context.Context, db *sql.DB, userID, entryID int64) error {
	entry, err := store.GetEntry(ctx, db, userID, entryID)
	if err != nil {
		return err
	}
	if entry.EndAt == nil {
		return ErrRunning
	}
	return nil
}

// Update rewrites a closed entry: project must be the user's own and active,
// and start_at must precede end_at. Running entries are never touched.
func (s *Service) Update(ctx context.Context, userID, entryID, projectID int64, startAt, endAt time.Time) error {
	if !startAt.Before(endAt) {
		return ErrInvalidRange
	}
	proj, err := store.GetProject(ctx, s.DB.SQL, userID, projectID)
	if err != nil {
		return err
	}
	if proj.RetiredAt != nil {
		return ErrRetired
	}
	if err := closedEntry(ctx, s.DB.SQL, userID, entryID); err != nil {
		return err
	}
	rows, err := store.UpdateClosedEntry(ctx, s.DB.SQL, userID, entryID, projectID, startAt, endAt)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if rows == 0 {
		return ErrRunning // the row was closed or deleted concurrently
	}
	return nil
}

// Delete removes a closed entry row; running entries are never touched.
func (s *Service) Delete(ctx context.Context, userID, entryID int64) error {
	if err := closedEntry(ctx, s.DB.SQL, userID, entryID); err != nil {
		return err
	}
	rows, err := store.DeleteClosedEntry(ctx, s.DB.SQL, userID, entryID)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if rows == 0 {
		return ErrRunning // a concurrent delete won the race
	}
	return nil
}
