package web

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"example.com/timetracker/internal/auth"
	"example.com/timetracker/internal/projects"
	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/timer"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed web/app.css web/app.js
var assetFS embed.FS

// App owns the HTTP surface: routes, session + CSRF middleware, and the
// embedded templates and assets.
type App struct {
	db       *store.DB
	auth     *auth.Service
	projects *projects.Service
	timer    *timer.Service
	tmpl     *template.Template
}

// New builds the app; hasher.Iterations is injected (600000 in production,
// 1000 in tests).
func New(db *store.DB, hasher auth.Hasher) *App {
	a := &App{
		db:       db,
		auth:     &auth.Service{DB: db, Hasher: hasher},
		projects: &projects.Service{DB: db},
		timer:    &timer.Service{DB: db},
	}
	a.tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))
	return a
}

// Handler wires every route; all state changes are POST + PRG.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.requireUser(a.handleIndex))
	mux.HandleFunc("GET /login", a.handleAuthPage("login.html"))
	mux.HandleFunc("POST /login", a.handleLoginPOST)
	mux.HandleFunc("GET /register", a.handleAuthPage("register.html"))
	mux.HandleFunc("POST /register", a.handleRegisterPOST)
	mux.HandleFunc("POST /logout", a.requireUser(a.handleLogout))
	mux.HandleFunc("POST /projects", a.requireUser(a.handleProjectCreate))
	mux.HandleFunc("POST /projects/{id}/rename", a.requireUser(a.handleProjectRename))
	mux.HandleFunc("POST /projects/{id}/retire", a.requireUser(a.handleProjectRetire))
	mux.HandleFunc("POST /projects/{id}/unretire", a.requireUser(a.handleProjectUnretire))
	mux.HandleFunc("POST /timer/start/{id}", a.requireUser(a.handleTimerStart))
	mux.HandleFunc("POST /timer/stop", a.requireUser(a.handleTimerStop))
	mux.HandleFunc("POST /entries/{id}/update", a.requireUser(a.handleEntryUpdate))
	mux.HandleFunc("POST /entries/{id}/delete", a.requireUser(a.handleEntryDelete))
	mux.HandleFunc("GET /assets/app.css", a.handleAsset("web/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /assets/app.js", a.handleAsset("web/app.js", "text/javascript; charset=utf-8"))
	return a.loadSession(a.csrf(mux))
}

func (a *App) handleAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := assetFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}
}

func cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

func requestSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestSecure(r),
		MaxAge:   int(maxAge.Seconds()),
	})
}

func clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	setCookie(w, r, name, "", -1)
}

// pathID parses the {id} route segment; malformed ids are 404 so forged ids
// never leak existence information.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
