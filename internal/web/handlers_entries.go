package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/timer"
)

// maxTZOffsetMinutes bounds the client-supplied timezone offset to the real
// world (UTC+14 = 840); larger values are crafted input and rejected.
const maxTZOffsetMinutes = 840

func (a *App) handleEntryUpdate(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	start, end, err := entryFormTimes(r)
	if err == nil {
		projectID, perr := strconv.ParseInt(r.FormValue("project_id"), 10, 64)
		if perr != nil || projectID < 1 {
			a.flashRedirect(w, r, "err", "Pick a project")
			return
		}
		err = a.timer.Update(r.Context(), user.ID, id, projectID, start, end)
	}
	switch {
	case err == nil:
		a.flashRedirect(w, r, "ok", "Entry saved")
	case errors.Is(err, errInvalidOffset):
		a.flashRedirect(w, r, "err", "Invalid timezone offset")
	case errors.Is(err, errInvalidWallClock):
		a.flashRedirect(w, r, "err", "Enter valid date and time values")
	case errors.Is(err, timer.ErrInvalidRange):
		a.flashRedirect(w, r, "err", "Start must be before end")
	case errors.Is(err, timer.ErrRetired):
		a.flashRedirect(w, r, "err", "Cannot move an entry to a retired project")
	case errors.Is(err, timer.ErrRunning):
		a.flashRedirect(w, r, "err", "Running entries cannot be edited")
	default:
		a.replyEntryError(w, r, err)
	}
}

func (a *App) handleEntryDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch err := a.timer.Delete(r.Context(), user.ID, id); {
	case err == nil:
		a.flashRedirect(w, r, "ok", "Entry deleted")
	case errors.Is(err, timer.ErrRunning):
		a.flashRedirect(w, r, "err", "Running entries cannot be deleted")
	default:
		a.replyEntryError(w, r, err)
	}
}

func (a *App) replyEntryError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	internalError(w, r, err)
}

// entryFormTimes converts each datetime-local wall-clock field to UTC using
// its paired hidden offset field: UTC = wall - offset. The offsets default to
// time.Local's offset at each stored instant (see viewForEntry), so a
// render-then-submit round trip without JS is an identity per field even
// across DST transitions; the enhancement script rewrites each offset only
// when it rewrites that field's value from the same instant.
func entryFormTimes(r *http.Request) (start, end time.Time, err error) {
	startOff, err := parseOffset(r.FormValue("start_offset_minutes"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endOff, err := parseOffset(r.FormValue("end_offset_minutes"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if start, err = parseWallField(r.FormValue("start_at"), startOff); err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err = parseWallField(r.FormValue("end_at"), endOff)
	return start, end, err
}

// Sentinel input failures; handleEntryUpdate maps them to flash copy in the
// same switch that maps timer errors, keeping user-facing text in one place.
var (
	errInvalidOffset    = errors.New("invalid timezone offset")
	errInvalidWallClock = errors.New("invalid wall clock value")
)

func parseOffset(value string) (int, error) {
	offsetMin, err := strconv.Atoi(value)
	if err != nil || offsetMin < -maxTZOffsetMinutes || offsetMin > maxTZOffsetMinutes {
		return 0, errInvalidOffset
	}
	return offsetMin, nil
}

func parseWallField(value string, offsetMinutes int) (time.Time, error) {
	t, ok := parseWallClock(value, offsetMinutes)
	if !ok {
		return time.Time{}, errInvalidWallClock
	}
	return t, nil
}

func parseWallClock(value string, offsetMinutes int) (time.Time, bool) {
	t, err := time.Parse(wallLayout, value)
	if err != nil {
		return time.Time{}, false
	}
	return t.Add(-time.Duration(offsetMinutes) * time.Minute).UTC(), true
}
