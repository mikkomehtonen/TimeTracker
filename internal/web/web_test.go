package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/timetracker/internal/auth"
	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/web"
)

type tester struct {
	t       *testing.T
	handler http.Handler
	db      *store.DB
}

func newTester(t *testing.T) *tester {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := web.New(db, auth.Hasher{Iterations: 1000})
	return &tester{t: t, handler: app.Handler(), db: db}
}

type jar map[string]string

func (j jar) apply(req *http.Request) {
	for k, v := range j {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
}

func (j jar) absorb(resp *http.Response) {
	for _, c := range resp.Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			delete(j, c.Name)
			continue
		}
		j[c.Name] = c.Value
	}
}

func (tr *tester) do(method, path string, form url.Values, j jar) *httptest.ResponseRecorder {
	tr.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	j.apply(req)
	rec := httptest.NewRecorder()
	tr.handler.ServeHTTP(rec, req)
	j.absorb(rec.Result())
	return rec
}

func (tr *tester) body(rec *httptest.ResponseRecorder) string {
	return rec.Body.String()
}

// registerAndLogin performs the full HTTP signup + sign-in dance and returns
// the session cookie jar.
func (tr *tester) registerAndLogin(username, password string) jar {
	tr.t.Helper()
	j := jar{}
	if rec := tr.do("GET", "/login", nil, j); rec.Code != http.StatusOK {
		tr.t.Fatalf("GET /login: %d", rec.Code)
	}
	csrf := j[auth.CSRFCookie]
	if csrf == "" {
		tr.t.Fatal("GET /login did not set the tt_csrf cookie")
	}
	form := url.Values{"csrf": {csrf}, "username": {username}, "password": {password}}
	if rec := tr.do("POST", "/register", form, j); rec.Code != http.StatusFound {
		tr.t.Fatalf("POST /register: %d", rec.Code)
	}
	tr.do("GET", "/login", nil, j)
	form = url.Values{"csrf": {j[auth.CSRFCookie]}, "username": {username}, "password": {password}}
	rec := tr.do("POST", "/login", form, j)
	if rec.Code != http.StatusFound {
		tr.t.Fatalf("POST /login: %d body=%s", rec.Code, tr.body(rec))
	}
	if j[auth.SessionCookie] == "" {
		tr.t.Fatal("login did not set the tt_session cookie")
	}
	return j
}

// wantRedirect asserts a 302 PRG response redirects to want, naming the op.
func (tr *tester) wantRedirect(rec *httptest.ResponseRecorder, want, op string) {
	tr.t.Helper()
	if rec.Code != http.StatusFound {
		tr.t.Fatalf("%s = %d, want 302", op, rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != want {
		tr.t.Errorf("%s Location = %q, want %s", op, loc, want)
	}
}

// authedPOST posts a form carrying the per-session CSRF token.
func (tr *tester) authedPOST(path string, values url.Values, j jar) *httptest.ResponseRecorder {
	tr.t.Helper()
	form := url.Values{}
	for k, v := range values {
		form[k] = v
	}
	form.Set("csrf", j[auth.SessionCookie])
	return tr.do("POST", path, form, j)
}

func (tr *tester) createProject(name string, j jar) {
	tr.t.Helper()
	if rec := tr.authedPOST("/projects", url.Values{"name": {name}}, j); rec.Code != http.StatusFound {
		tr.t.Fatalf("POST /projects: %d", rec.Code)
	}
}

// insertClosed seeds a closed entry via raw SQL: the service-level update
// path excludes running rows, so there is no supported way to close one.
func (tr *tester) insertClosed(userID, projectID int64, start, end time.Time) int64 {
	tr.t.Helper()
	ctx := context.Background()
	id, err := store.InsertEntry(ctx, tr.db.SQL, userID, projectID, start)
	if err != nil {
		tr.t.Fatalf("insert entry: %v", err)
	}
	if _, err := tr.db.SQL.ExecContext(ctx,
		`UPDATE entries SET end_at = ? WHERE id = ?`, store.FormatTime(end), id); err != nil {
		tr.t.Fatalf("close entry: %v", err)
	}
	return id
}

func (tr *tester) count(table, userCol string, id int64) int {
	tr.t.Helper()
	var n int
	q := `SELECT count(*) FROM ` + table
	args := []any{}
	if userCol != "" {
		q += ` WHERE ` + userCol + ` = ?`
		args = append(args, id)
	}
	if err := tr.db.SQL.QueryRow(q, args...).Scan(&n); err != nil {
		tr.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (tr *tester) userID(username string) int64 {
	tr.t.Helper()
	u, err := store.GetUserByName(context.Background(), tr.db.SQL, username)
	if err != nil {
		tr.t.Fatalf("user %s: %v", username, err)
	}
	return u.ID
}

func TestUnauthenticatedIndexRedirects(t *testing.T) {
	tr := newTester(t)
	rec := tr.do("GET", "/", nil, jar{})
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestCreateProjectRendersOnPage(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	rec := tr.authedPOST("/projects", url.Values{"name": {"Web site"}}, j)
	tr.wantRedirect(rec, "/", "POST /projects")
	if n := tr.count("projects", "user_id", tr.userID("alice")); n != 1 {
		t.Fatalf("project rows = %d, want 1", n)
	}
	rec = tr.do("GET", "/", nil, j)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
	}
	if !strings.Contains(tr.body(rec), "Web site") {
		t.Error("page body does not contain the new project")
	}
}

func TestDuplicateProjectShowsFlashAndKeepsOneRow(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Web site", j)
	rec := tr.authedPOST("/projects", url.Values{"name": {"web site"}}, j)
	tr.wantRedirect(rec, "/", "duplicate POST /projects")
	if n := tr.count("projects", "user_id", tr.userID("alice")); n != 1 {
		t.Fatalf("project rows = %d, want exactly 1", n)
	}
	rec = tr.do("GET", "/", nil, j)
	if !strings.Contains(tr.body(rec), "A project with this name already exists") {
		t.Errorf("flash error not rendered; body=%s", firstChunk(tr.body(rec)))
	}
}

func TestCSRFMissingOrWrongIs403WithNoStateChange(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	rec := tr.do("POST", "/projects", url.Values{"name": {"Sneaky"}}, j)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without csrf = %d, want 403", rec.Code)
	}
	wrong := url.Values{"name": {"Sneaky"}, "csrf": {"deadbeef"}}
	rec = tr.do("POST", "/projects", wrong, j)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with wrong csrf = %d, want 403", rec.Code)
	}
	if n := tr.count("projects", "user_id", tr.userID("alice")); n != 0 {
		t.Errorf("project rows = %d after CSRF rejections, want 0", n)
	}
}

func TestLoginPOSTWithoutCSRFCookieIs403(t *testing.T) {
	tr := newTester(t)
	form := url.Values{"csrf": {"whatever"}, "username": {"alice"}, "password": {"password1234"}}
	rec := tr.do("POST", "/login", form, jar{})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestTimerStartThenStopFlow(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	pid := tr.projectID("alice", "Alpha")

	if rec := tr.authedPOST("/timer/start/"+itoa(pid), nil, j); rec.Code != http.StatusFound {
		t.Fatalf("start = %d", rec.Code)
	}
	rec := tr.do("GET", "/", nil, j)
	if !strings.Contains(tr.body(rec), "Tracking") || !strings.Contains(tr.body(rec), "Stop timer") {
		t.Errorf("running timer bar missing; body=%s", firstChunk(tr.body(rec)))
	}
	if n := tr.count("entries", "user_id", tr.userID("alice")); n != 1 {
		t.Fatalf("running entries = %d, want 1", n)
	}
	if rec := tr.authedPOST("/timer/stop", nil, j); rec.Code != http.StatusFound {
		t.Fatalf("stop = %d", rec.Code)
	}
	rec = tr.do("GET", "/", nil, j)
	body := tr.body(rec)
	if !strings.Contains(body, "No timer running") {
		t.Errorf("idle timer bar missing after stop; body=%s", firstChunk(body))
	}
	// The per-project start forms render in both states; the state-specific
	// transition is the stop control disappearing once the timer closes.
	if strings.Contains(body, "Stop timer") {
		t.Errorf("stop control still rendered in idle state; body=%s", firstChunk(body))
	}
	if !strings.Contains(body, "/timer/start/"+itoa(pid)) ||
		!strings.Contains(body, "Start tracking</button>") {
		t.Errorf("per-project start form missing; body=%s", firstChunk(body))
	}
	var closed int
	if err := tr.db.SQL.QueryRow(`SELECT count(*) FROM entries WHERE end_at IS NOT NULL`).Scan(&closed); err != nil {
		t.Fatalf("count closed: %v", err)
	}
	if closed != 1 {
		t.Errorf("closed entries = %d, want 1", closed)
	}
}

func TestTimerStartForeignProjectIs404(t *testing.T) {
	tr := newTester(t)
	jA := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Secret", jA)
	pid := tr.projectID("alice", "Secret")

	jB := tr.registerAndLogin("bob", "password1234")
	rec := tr.authedPOST("/timer/start/"+itoa(pid), nil, jB)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if n := tr.count("entries", "user_id", tr.userID("bob")); n != 0 {
		t.Errorf("bob entries = %d, want 0", n)
	}
}

func TestProjectNameIsHTMLEscaped(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("<b>x</b>", j)
	rec := tr.do("GET", "/", nil, j)
	body := tr.body(rec)
	if !strings.Contains(body, "&lt;b&gt;x&lt;/b&gt;") {
		t.Error("escaped project name not found in body")
	}
	if strings.Contains(body, "<b>x</b>") {
		t.Error("raw user-supplied <b> element found in body")
	}
}

func TestEntryUpdateAppliesTimezoneOffset(t *testing.T) {
	tr := newTester(t)
	ctx := context.Background()
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	uid := tr.userID("alice")
	pid := tr.projectID("alice", "Alpha")

	start := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	id := tr.insertClosed(uid, pid, start, end)

	form := url.Values{
		"project_id": {itoa(pid)},
		// Per-field independence: start parsed at PST (-480), end at PDT
		// (-420); a shared-offset implementation could not produce both.
		"start_at":             {"2026-05-01T10:00:00"},
		"end_at":               {"2026-05-01T12:00:00"},
		"start_offset_minutes": {"-480"},
		"end_offset_minutes":   {"-420"},
	}
	if rec := tr.authedPOST("/entries/"+itoa(id)+"/update", form, j); rec.Code != http.StatusFound {
		t.Fatalf("update = %d", rec.Code)
	}
	got, err := store.GetEntry(ctx, tr.db.SQL, uid, id)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	wantStart := time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 5, 1, 19, 0, 0, 0, time.UTC)
	if !got.StartAt.Equal(wantStart) {
		t.Errorf("start_at = %s, want %s (value + 8h)", got.StartAt, wantStart)
	}
	if got.EndAt == nil || !got.EndAt.Equal(wantEnd) {
		t.Errorf("end_at = %v, want %s", got.EndAt, wantEnd)
	}
}

func (tr *tester) projectID(username, name string) int64 {
	tr.t.Helper()
	var id int64
	err := tr.db.SQL.QueryRow(`SELECT p.id FROM projects p JOIN users u ON u.id = p.user_id
		WHERE u.username = ? AND p.name = ?`, username, name).Scan(&id)
	if err != nil {
		tr.t.Fatalf("project %s/%s: %v", username, name, err)
	}
	return id
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func firstChunk(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}

// TestNoJSEditRoundTripIsIdentity covers the no-JS contract: each datetime
// field is paired with a hidden offset holding the server zone offset *at
// that stored instant*, so resubmitting the rendered values unchanged must
// restore the exact stored instants on any server zone - including entries
// straddling a DST transition. Zones are forced non-UTC so the scraped
// offsets are meaningful (UTC CI would make the assertions tautological).
func scrapeEntryForm(t *testing.T, tr *tester, j jar, zone string) (startOff, endOff, startVal, endVal string) {
	t.Helper()
	body := tr.body(tr.do("GET", "/", nil, j))
	startOff = scrape(body, `name="start_offset_minutes" value="(-?\d+)"`)
	endOff = scrape(body, `name="end_offset_minutes" value="(-?\d+)"`)
	startVal = scrape(body, `name="start_at"[^>]*value="([^"]+)"`)
	endVal = scrape(body, `name="end_at"[^>]*value="([^"]+)"`)
	if startOff == "" || endOff == "" || startVal == "" || endVal == "" {
		t.Fatalf("%s: entry form fields not found in body", zone)
	}
	return startOff, endOff, startVal, endVal
}

type roundTripCase struct {
	name        string
	zone        string
	start, end  time.Time
	wantOffsets [2]int
}

func TestNoJSEditRoundTripIsIdentity(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	uid := tr.userID("alice")
	pid := tr.projectID("alice", "Alpha")

	for _, tc := range []roundTripCase{
		{
			name:        "newyork-winter-offset-pinned",
			zone:        "America/New_York",
			start:       time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
			end:         time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
			wantOffsets: [2]int{-240, -240},
		},
		{
			name:        "kolkata-half-hour-offset",
			zone:        "Asia/Kolkata",
			start:       time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
			end:         time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
			wantOffsets: [2]int{330, 330},
		},
		{
			name:        "newyork-straddles-2026-03-08-dst",
			zone:        "America/New_York",
			start:       time.Date(2026, 3, 8, 5, 30, 0, 0, time.UTC),
			end:         time.Date(2026, 3, 8, 15, 30, 0, 0, time.UTC),
			wantOffsets: [2]int{-300, -240},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roundTripCaseRun(t, tr, j, uid, pid, tc)
		})
	}
}

func roundTripCaseRun(t *testing.T, tr *tester, j jar, uid, pid int64, tc roundTripCase) {
	prev := time.Local
	loc, err := time.LoadLocation(tc.zone)
	if err != nil {
		t.Fatalf("load %s: %v", tc.zone, err)
	}
	time.Local = loc
	defer func() { time.Local = prev }()
	id := tr.insertClosed(uid, pid, tc.start, tc.end)
	t.Cleanup(func() {
		if _, err := tr.db.SQL.ExecContext(context.Background(), "DELETE FROM entries WHERE id = ?", id); err != nil {
			t.Errorf("cleanup entry: %v", err)
		}
	})

	startOff, endOff, startVal, endVal := scrapeEntryForm(t, tr, j, tc.zone)
	if atoi(startOff) != tc.wantOffsets[0] || atoi(endOff) != tc.wantOffsets[1] {
		t.Errorf("%s: rendered offsets = %s/%s, want %v (zero offsets would make this test tautological)",
			tc.zone, startOff, endOff, tc.wantOffsets)
	}
	form := url.Values{
		"project_id":           {itoa(pid)},
		"start_at":             {startVal},
		"end_at":               {endVal},
		"start_offset_minutes": {startOff},
		"end_offset_minutes":   {endOff},
	}
	if rec := tr.authedPOST("/entries/"+itoa(id)+"/update", form, j); rec.Code != http.StatusFound {
		t.Fatalf("%s: update = %d", tc.zone, rec.Code)
	}
	got, err := store.GetEntry(context.Background(), tr.db.SQL, uid, id)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if !got.StartAt.Equal(tc.start) {
		t.Errorf("%s: start_at drifted to %v, want %v", tc.zone, got.StartAt, tc.start)
	}
	if got.EndAt == nil || !got.EndAt.Equal(tc.end) {
		t.Errorf("%s: end_at drifted to %v, want %v", tc.zone, got.EndAt, tc.end)
	}
	if _, err := tr.db.SQL.Exec("DELETE FROM entries WHERE id = ?", id); err != nil {
		t.Fatalf("cleanup entry: %v", err)
	}
}

func scrape(body, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func TestOutOfClockzoneOffsetIsRejected(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	uid := tr.userID("alice")
	pid := tr.projectID("alice", "Alpha")
	start := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	id := tr.insertClosed(uid, pid, start, end)

	for _, field := range []string{"start_offset_minutes", "end_offset_minutes"} {
		for _, offset := range []string{"999999999999999999999", "999999999", "-4800", "abc"} {
			form := url.Values{
				"project_id":           {itoa(pid)},
				"start_at":             {"2026-05-01T10:00:00"},
				"end_at":               {"2026-05-01T12:00:00"},
				"start_offset_minutes": {"0"},
				"end_offset_minutes":   {"0"},
			}
			form.Set(field, offset)
			rec := tr.authedPOST("/entries/"+itoa(id)+"/update", form, j)
			if rec.Code != http.StatusFound {
				t.Fatalf("%s=%s: update = %d, want 302 flash", field, offset, rec.Code)
			}
			body := tr.body(tr.do("GET", "/", nil, j))
			if !strings.Contains(body, "Invalid timezone offset") {
				t.Errorf("%s=%s: invalid-offset flash not shown", field, offset)
			}
		}
	}
	got, err := store.GetEntry(context.Background(), tr.db.SQL, uid, id)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if !got.StartAt.Equal(start) {
		t.Errorf("start_at changed to %v despite rejected offsets", got.StartAt)
	}
}

// TestOffsetBoundaryIsInclusive pins the inclusive UTC+14 bound: +/-840 is
// accepted, +/-841 rejected.
func TestOffsetBoundaryIsInclusive(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	uid := tr.userID("alice")
	pid := tr.projectID("alice", "Alpha")
	start := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	id := tr.insertClosed(uid, pid, start, end)

	post := func(off string) *httptest.ResponseRecorder {
		form := url.Values{
			"project_id":           {itoa(pid)},
			"start_at":             {"2026-05-01T10:00:00"},
			"end_at":               {"2026-05-01T12:00:00"},
			"start_offset_minutes": {off},
			"end_offset_minutes":   {off},
		}
		return tr.authedPOST("/entries/"+itoa(id)+"/update", form, j)
	}
	for _, off := range []string{"841", "-841"} {
		tr.wantRedirect(post(off), "/", "POST with offset "+off)
		if !strings.Contains(tr.body(tr.do("GET", "/", nil, j)), "Invalid timezone offset") {
			t.Errorf("%s accepted; want invalid-offset flash", off)
		}
	}
	for _, tc := range []struct {
		off   string
		start time.Time
	}{
		// UTC = wall - offset: 10:00 wall at UTC+14 is 20:00Z the day before;
		// at UTC-14 it is 00:00Z the day after.
		{"840", time.Date(2026, 4, 30, 20, 0, 0, 0, time.UTC)},
		{"-840", time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)},
	} {
		tr.wantRedirect(post(tc.off), "/", "POST with offset "+tc.off)
		got, err := store.GetEntry(context.Background(), tr.db.SQL, uid, id)
		if err != nil {
			t.Fatalf("get entry: %v", err)
		}
		if !got.StartAt.Equal(tc.start) {
			t.Errorf("offset %s: start_at = %v, want %v", tc.off, got.StartAt, tc.start)
		}
	}
}

// TestRunningEntryNotEditableOrDeletableOverHTTP exercises the story rule at
// the endpoint level: the running row rejects update and delete with flashes,
// stays running, and nothing new is written.
func TestRunningEntryNotEditableOrDeletableOverHTTP(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	pid := tr.projectID("alice", "Alpha")
	if rec := tr.authedPOST("/timer/start/"+itoa(pid), nil, j); rec.Code != http.StatusFound {
		t.Fatalf("start = %d", rec.Code)
	}
	var runID int64
	if err := tr.db.SQL.QueryRow(
		`SELECT id FROM entries WHERE end_at IS NULL AND user_id = ?`,
		tr.userID("alice")).Scan(&runID); err != nil {
		t.Fatalf("running entry id: %v", err)
	}

	form := url.Values{
		"project_id":           {itoa(pid)},
		"start_at":             {"2026-05-01T10:00:00"},
		"end_at":               {"2026-05-01T12:00:00"},
		"start_offset_minutes": {"0"},
		"end_offset_minutes":   {"0"},
	}
	rec := tr.authedPOST("/entries/"+itoa(runID)+"/update", form, j)
	tr.wantRedirect(rec, "/", "update running entry")
	if !strings.Contains(tr.body(tr.do("GET", "/", nil, j)), "Running entries cannot be edited") {
		t.Error("edit flash not shown")
	}
	rec = tr.authedPOST("/entries/"+itoa(runID)+"/delete", nil, j)
	tr.wantRedirect(rec, "/", "delete running entry")
	if !strings.Contains(tr.body(tr.do("GET", "/", nil, j)), "Running entries cannot be deleted") {
		t.Error("delete flash not shown")
	}
	var running int
	if err := tr.db.SQL.QueryRow(
		`SELECT count(*) FROM entries WHERE end_at IS NULL AND id = ?`, runID).Scan(&running); err != nil {
		t.Fatalf("count running: %v", err)
	}
	if running != 1 {
		t.Errorf("running entry changed: count = %d, want 1 (untouched)", running)
	}
	if n := tr.count("entries", "user_id", tr.userID("alice")); n != 1 {
		t.Errorf("total entries = %d, want still 1", n)
	}
}
