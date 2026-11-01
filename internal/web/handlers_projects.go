package web

import (
	"errors"
	"net/http"
	"time"

	"example.com/timetracker/internal/projects"
	"example.com/timetracker/internal/store"
)

func (a *App) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	_, err := a.projects.Create(r.Context(), user.ID, r.FormValue("name"), time.Now())
	a.replyProjectResult(w, r, err, "Project created")
}

func (a *App) handleProjectRename(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, err := a.projects.Rename(r.Context(), user.ID, id, r.FormValue("name"))
	a.replyProjectResult(w, r, err, "Project renamed")
}

func (a *App) handleProjectRetire(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := a.projects.Retire(r.Context(), user.ID, id, time.Now())
	a.replyProjectResult(w, r, err, "Project retired")
}

func (a *App) handleProjectUnretire(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := a.projects.Unretire(r.Context(), user.ID, id)
	a.replyProjectResult(w, r, err, "Project un-retired")
}

// replyProjectResult maps domain errors to flashes and PRG, and foreign
// resources to 404 (never 403).
func (a *App) replyProjectResult(w http.ResponseWriter, r *http.Request, err error, okMsg string) {
	switch {
	case err == nil:
		a.flashRedirect(w, r, "ok", okMsg)
	case errors.Is(err, projects.ErrInvalidName):
		a.flashRedirect(w, r, "err", "Name must be 1–100 characters")
	case errors.Is(err, projects.ErrDuplicate):
		a.flashRedirect(w, r, "err", "A project with this name already exists")
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(w, r)
	default:
		internalError(w, r, err)
	}
}

func (a *App) flashRedirect(w http.ResponseWriter, r *http.Request, kind, msg string) {
	setFlash(w, r, kind, msg)
	http.Redirect(w, r, "/", http.StatusFound)
}
