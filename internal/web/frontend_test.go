package web_test

import (
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"example.com/timetracker/internal/store"
)

const viewportMeta = `<meta name="viewport" content="width=device-width, initial-scale=1">`

func TestPagesCarryViewportAndSingleStylesheet(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	pages := []struct {
		path string
		jar  jar
	}{
		{"/login", jar{}},
		{"/", j},
	}
	for _, p := range pages {
		rec := tr.do("GET", p.path, nil, p.jar)
		body := tr.body(rec)
		if !strings.Contains(body, viewportMeta) {
			t.Errorf("GET %s: viewport meta missing", p.path)
		}
		links := regexp.MustCompile(`<link[^>]*stylesheet[^>]*>`).FindAllString(body, -1)
		if len(links) != 1 {
			t.Fatalf("GET %s: stylesheet links = %d, want exactly 1", p.path, len(links))
		}
		href := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(links[0])
		if href == nil {
			t.Fatalf("GET %s: stylesheet link has no href: %s", p.path, links[0])
		}
		css := tr.do("GET", href[1], nil, jar{})
		if css.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", href[1], css.Code)
		}
	}
}

func TestRunningEntryRendersRFC3339TimeAndStaticDuration(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	pid := tr.projectID("alice", "Alpha")
	if rec := tr.authedPOST("/timer/start/"+itoa(pid), nil, j); rec.Code != http.StatusFound {
		t.Fatalf("start = %d", rec.Code)
	}
	body := tr.body(tr.do("GET", "/", nil, j))

	m := regexp.MustCompile(`<time[^>]*datetime="([^"]+)"[^>]*>([^<]*)</time>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no <time datetime=...> element found in running timer markup")
	}
	if !strings.HasSuffix(m[1], "Z") {
		t.Errorf("datetime %q is not UTC RFC 3339", m[1])
	}
	if _, err := store.ParseTime(m[1]); err != nil {
		t.Errorf("datetime %q unparseable: %v", m[1], err)
	}
	if m[2] == "" {
		t.Error("running timer markup has no server-rendered duration (JS-free fallback)")
	}
}

func TestNoInlineEventHandlers(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	tr.authedPOST("/timer/start/"+itoa(tr.projectID("alice", "Alpha")), nil, j)
	for _, path := range []string{"/", "/login", "/register"} {
		body := tr.body(tr.do("GET", path, nil, j))
		if on := regexp.MustCompile(`[\s"]on[a-z]+=`).FindString(body); on != "" {
			t.Errorf("GET %s contains inline event handler %q", path, on)
		}
	}
}

func TestEntryFormsCarryDataUTCAndLocalizableTimes(t *testing.T) {
	tr := newTester(t)
	j := tr.registerAndLogin("alice", "password1234")
	tr.createProject("Alpha", j)
	uid := tr.userID("alice")
	pid := tr.projectID("alice", "Alpha")

	start := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	tr.insertClosed(uid, pid, start, start.Add(time.Hour))
	body := tr.body(tr.do("GET", "/", nil, j))
	if !strings.Contains(body, `data-utc="`) {
		t.Error("entry edit form lacks data-utc anchor for JS localization")
	}
	if !strings.Contains(body, `class="localizable"`) {
		t.Error("entry list lacks localizable <time> elements")
	}
}

// TestApplyTimezoneWritesPerFieldOffsets runs the real app.js under node with
// a minimal DOM shim, pinning the DST-correct contract: each datetime field's
// hidden offset (paired via data-offset) is rewritten from the *same instant*
// the field is rendered from, so a winter and a summer instant in a DST zone
// get different offsets and no stored instant drifts.
func TestApplyTimezoneWritesPerFieldOffsets(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	shim := `
class El {
  constructor(attrs) { this.attrs = attrs; this.value = ""; }
  getAttribute(n) { return n in this.attrs ? this.attrs[n] : null; }
}
const byId = {};
function add(el) { byId[el.attrs.id] = el; return el; }
const dt = [
  add(new El({id: "s", type: "datetime-local", "data-utc": "2026-01-10T08:00:00.000000000Z", "data-offset": "so"})),
  add(new El({id: "e", type: "datetime-local", "data-utc": "2026-07-10T08:00:00.000000000Z", "data-offset": "eo"})),
  new El({id: "orphan", type: "datetime-local", "data-utc": "2026-01-10T08:00:00.000000000Z", "data-offset": "missing"}),
];
const off = [add(new El({id: "so"})), add(new El({id: "eo"}))];
global.document = {
  querySelectorAll: (sel) => (sel.includes("datetime-local") ? dt : []),
  getElementById: (id) => byId[id] || null,
};
global.setInterval = () => 0;
`
	runner := shim + string(src) + `
const assert = (cond, msg) => { if (!cond) { throw new Error(msg); } };
assert(dt[0].value === "2026-01-10T09:00:00", "start wall = " + dt[0].value);
assert(dt[1].value === "2026-07-10T10:00:00", "end wall = " + dt[1].value);
assert(off[0].value === "60", "start offset = " + off[0].value + ", want 60 (CET)");
assert(off[1].value === "120", "end offset = " + off[1].value + ", want 120 (CEST)");
// An unpaired field (data-offset target absent) must be left untouched:
// rewriting only the wall value would desynchronize the pair.
assert(dt[2].value === "", "orphan field rewritten to " + dt[2].value);
`
	cmd := exec.Command(node, "-e", runner)
	cmd.Env = append(os.Environ(), "TZ=Europe/Berlin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("app.js contract violated under TZ=Europe/Berlin: %v\n%s", err, out)
	}
}
