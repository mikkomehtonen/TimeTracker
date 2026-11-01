package web

import (
	"errors"
	"net/http"
	"time"

	"example.com/timetracker/internal/auth"
)

// handleAuthPage serves /login and /register, issuing the short-lived
// tt_csrf cookie for the unauthenticated forms.
func (a *App) handleAuthPage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentUser(r); ok {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		d := &pageData{CSRF: cookieValue(r, auth.CSRFCookie)}
		if d.CSRF == "" {
			token, err := auth.NewCSRFToken()
			if err != nil {
				internalError(w, r, err)
				return
			}
			setCookie(w, r, auth.CSRFCookie, token, auth.CSRFTTL)
			d.CSRF = token
		}
		a.render(w, r, name, d)
	}
}

func (a *App) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	token, expires, err := a.auth.Login(r.Context(),
		r.FormValue("username"), r.FormValue("password"), time.Now())
	if err != nil {
		if !errors.Is(err, auth.ErrBadCredentials) {
			internalError(w, r, err)
			return
		}
		setFlash(w, r, "err", "Invalid username or password")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	setCookie(w, r, auth.SessionCookie, token, time.Until(expires))
	clearCookie(w, r, auth.CSRFCookie)
	setFlash(w, r, "ok", "Signed in")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleRegisterPOST(w http.ResponseWriter, r *http.Request) {
	_, err := a.auth.Register(r.Context(),
		r.FormValue("username"), r.FormValue("password"), time.Now())
	switch {
	case err == nil:
		setFlash(w, r, "ok", "Account created, sign in below")
	case errors.Is(err, auth.ErrDuplicateUser):
		setFlash(w, r, "err", "That username is taken")
	case errors.Is(err, auth.ErrInvalidInput):
		setFlash(w, r, "err", "Username required; password must be at least 8 characters")
	default:
		internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.Destroy(r.Context(), cookieValue(r, auth.SessionCookie)); err != nil {
		internalError(w, r, err)
		return
	}
	clearCookie(w, r, auth.SessionCookie)
	setFlash(w, r, "ok", "Signed out")
	http.Redirect(w, r, "/login", http.StatusFound)
}
