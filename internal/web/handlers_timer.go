package web

import (
	"errors"
	"net/http"
	"time"

	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/timer"
)

func (a *App) handleTimerStart(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := a.timer.Start(r.Context(), user.ID, id, time.Now())
	switch {
	case err == nil:
		a.flashRedirect(w, r, "ok", "Timer started")
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, timer.ErrRetired):
		a.flashRedirect(w, r, "err", "That project is retired")
	case errors.Is(err, timer.ErrConflict):
		a.flashRedirect(w, r, "err", "Timer changed, please retry")
	default:
		internalError(w, r, err)
	}
}

func (a *App) handleTimerStop(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	if err := a.timer.Stop(r.Context(), user.ID, time.Now()); err != nil {
		internalError(w, r, err)
		return
	}
	a.flashRedirect(w, r, "ok", "Timer stopped")
}
