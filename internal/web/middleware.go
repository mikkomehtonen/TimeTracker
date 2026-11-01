package web

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

	"example.com/timetracker/internal/auth"
	"example.com/timetracker/internal/store"
)

type contextKey int

const userKey contextKey = iota

// loadSession resolves the tt_session cookie into the context user. Expired
// session rows are deleted opportunistically (auth.Resolve).
func (a *App) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := cookieValue(r, auth.SessionCookie)
		if token != "" {
			user, err := a.auth.Resolve(r.Context(), token, time.Now())
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, user))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) (store.User, bool) {
	u, ok := r.Context().Value(userKey).(store.User)
	return u, ok
}

func (a *App) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		h(w, r)
	}
}

// csrf rejects every POST whose hidden csrf field is missing or does not
// match the request's token: 403 with no state change. Authenticated requests
// use the per-session token; unauthenticated login/register posts use the
// short-lived tt_csrf cookie issued at GET time.
func (a *App) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "malformed form", http.StatusBadRequest)
			return
		}
		token := r.FormValue("csrf")
		if token == "" || !a.csrfMatches(r, token) {
			http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) csrfMatches(r *http.Request, token string) bool {
	if _, ok := currentUser(r); ok {
		return constantEqual(token, cookieValue(r, auth.SessionCookie))
	}
	issued := cookieValue(r, auth.CSRFCookie)
	return issued != "" && constantEqual(token, issued)
}

func constantEqual(x, y string) bool {
	return subtle.ConstantTimeCompare([]byte(x), []byte(y)) == 1
}
