package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"example.com/timetracker/internal/auth"
	"example.com/timetracker/internal/store"
)

// wallLayout matches datetime-local values (server-local fallback when JS is
// off; browser-local after the enhancement script rewrites the fields).
const wallLayout = "2006-01-02T15:04:05"

type runningView struct {
	ProjectName  string
	SinceLocal   string
	StartRFC3339 string
	Elapsed      string
}

type entryView struct {
	ID          int64
	ProjectID   int64
	ProjectName string
	StartRFC    string
	EndRFC      string
	StartLocal  string
	EndLocal    string
	Duration    string
	// StartOffsetMinutes/EndOffsetMinutes are time.Local's UTC offsets at
	// each stored instant, pre-filled into hidden fields paired with the
	// datetime inputs. A render-then-submit round trip without JS restores
	// both instants exactly, even when the entry straddles a DST transition;
	// the enhancement script rewrites each offset together with its field.
	// Known limitation: offsets stay pinned to the *stored* instants, so a
	// manually retyped wall time landing on the other side of a DST change
	// (or in a repeated hour) shifts by the DST delta; closing that fully
	// means shipping IANA zone names and resolving with time.LoadLocation.
	StartOffsetMinutes int
	EndOffsetMinutes   int
}

type pageData struct {
	Username  string
	CSRF      string
	FlashKind string
	Flash     string
	Running   *runningView
	Active    []store.Project
	Retired   []store.Project
	Entries   []entryView
}

// render executes a template and consumes any pending flash exactly once.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, d *pageData) {
	if kind, msg, ok := takeFlash(r); ok {
		d.FlashKind, d.Flash = kind, msg
		clearCookie(w, r, flashCookie)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, name, d); err != nil {
		slog.Error("template render failed", "template", name, "err", err)
	}
}

func (a *App) newPageData(r *http.Request) (pageData, error) {
	user, _ := currentUser(r)
	d := pageData{Username: user.Username, CSRF: cookieValue(r, auth.SessionCookie)}
	active, retired, err := a.projects.List(r.Context(), user.ID)
	if err != nil {
		return d, err
	}
	d.Active, d.Retired = active, retired
	if err := a.fillRunning(r, user.ID, &d); err != nil {
		return d, err
	}
	return d, a.fillEntries(r, user.ID, &d)
}

func (a *App) fillRunning(r *http.Request, userID int64, d *pageData) error {
	entry, ok, err := a.timer.Running(r.Context(), userID)
	if err != nil || !ok {
		return err
	}
	start := entry.StartAt
	d.Running = &runningView{
		ProjectName:  projectName(d, entry.ProjectID),
		SinceLocal:   wallLocal(start),
		StartRFC3339: utcStamp(start),
		Elapsed:      formatElapsed(time.Since(start)),
	}
	return nil
}

func (a *App) fillEntries(r *http.Request, userID int64, d *pageData) error {
	entries, err := a.timer.RecentClosed(r.Context(), userID)
	if err != nil {
		return err
	}
	for _, e := range entries {
		d.Entries = append(d.Entries, viewForEntry(d, e))
	}
	return nil
}

func viewForEntry(d *pageData, e store.Entry) entryView {
	end := e.StartAt
	if e.EndAt != nil {
		end = *e.EndAt
	}
	return entryView{
		ID:                 e.ID,
		ProjectID:          e.ProjectID,
		ProjectName:        projectName(d, e.ProjectID),
		StartRFC:           utcStamp(e.StartAt),
		EndRFC:             utcStamp(end),
		StartLocal:         wallLocal(e.StartAt),
		EndLocal:           wallLocal(end),
		Duration:           formatElapsed(end.Sub(e.StartAt)),
		StartOffsetMinutes: localOffsetMinutes(e.StartAt),
		EndOffsetMinutes:   localOffsetMinutes(end),
	}
}

// localOffsetMinutes is the zone offset (east-positive) of time.Local at t.
func localOffsetMinutes(t time.Time) int {
	_, offset := t.In(time.Local).Zone()
	return offset / 60
}

func projectName(d *pageData, id int64) string {
	for _, p := range d.Active {
		if p.ID == id {
			return p.Name
		}
	}
	for _, p := range d.Retired {
		if p.ID == id {
			return p.Name
		}
	}
	return "Unknown project"
}

func wallLocal(t time.Time) string { return t.In(time.Local).Format(wallLayout) }

// utcStamp renders t in store.FormatTime's canonical UTC layout, a valid
// RFC 3339 instant with a fixed 9-digit fraction, used in datetime attributes.
func utcStamp(t time.Time) string { return store.FormatTime(t) }

// formatElapsed renders h:mm:ss or m:ss, clamped at zero.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Seconds())
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
