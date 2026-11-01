# Multi-user Time Tracker Web App

## Context

The repo is a greenfield Go starter: `cmd/timetracker/main.go` only prints "TimeTracker", there is no `internal/`, no tests, no frontend, zero dependencies. The user wants a real product: a mobile-first (but desktop-usable) web app where multiple users manage projects and track time on them. This story builds the whole v1: server-rendered web UI, auth, SQLite persistence, and the project/timer domain.

## Out of Scope

- Reports, totals, charts, week/month aggregation.
- Notes/descriptions on time entries; bulk operations on entries.
- Deleting projects (retirement replaces deletion).
- Password change/reset, email, OAuth, 2FA.
- Native mobile apps, offline mode, push sync across devices.
- Deployment tooling (Docker, reverse proxy config) beyond flags on the binary.

## Implementation approach

**Stack (user-chosen):** server-rendered web app using only `net/http`, `html/template`, `go:embed` from stdlib, plus one new module dependency: `modernc.org/sqlite` (pure Go, driver name `sqlite`). No Node toolchain, no JS framework; a single small vanilla JS file for progressive enhancement. Plain HTML form POSTs + Post/Redirect/Get with flash messages make every flow work without JS.

**Layout (new packages, all under `internal/` with colocated `_test.go`):**
- `internal/store` — opens the DB, runs idempotent migrations, holds all queries. `sql.Open("sqlite", filepath.Join(*dataDir, "timetracker.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")`, `db.SetMaxOpenConns(1)` (serializes writes; fine at this scale). Tests open a fresh file under `t.TempDir()`.
- `internal/auth` — registration, login, session tokens, password hashing.
- `internal/projects` — project CRUD + retire/un-retire domain rules.
- `internal/timer` — start/stop/switch commands.
- `internal/web` — handlers, session + CSRF middleware, embedded templates/assets.

**Schema (single migration run at startup, `CREATE TABLE IF NOT EXISTS`):**

```sql
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  passhash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE projects (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  retired_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (user_id, name COLLATE NOCASE)
);
CREATE TABLE entries (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  project_id INTEGER NOT NULL REFERENCES projects(id),
  start_at TEXT NOT NULL,
  end_at TEXT                      -- NULL = running
);
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,     -- SHA-256 of the cookie token
  user_id INTEGER NOT NULL REFERENCES users(id),
  expires_at TEXT NOT NULL
);
CREATE UNIQUE INDEX one_running_per_user ON entries (user_id) WHERE end_at IS NULL;
```

**Timestamp rule (explicit, so string comparison is safe):** every timestamp is UTC, stored as `t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")` (fixed 9-digit fraction). This makes lexicographic order identical to chronological order. Never use `time.RFC3339Nano` directly for storage (it trims trailing zeros).

**Password rule:** stdlib `crypto/pbkdf2` (available in Go 1.24), PBKDF2-HMAC-SHA256, 32-byte key, 16-byte `crypto/rand` salt, stored as `pbkdf2$sha256$<iterations>$<b64 salt>$<b64 key>`. Iteration count lives in the hash string so verification always re-derives with the recorded cost. Iterations are a field on an injected `auth.Hasher` type: production 600000, tests use 1000 to keep the suite fast. Compare with `crypto/subtle.ConstantTimeCompare`.

**Session rule:** 32 bytes from `crypto/rand`, hex-encoded as cookie `tt_session` (HttpOnly, SameSite=Lax, Path=/, Secure when the request arrives over TLS per `r.TLS`/`X-Forwarded-Proto`). DB stores only SHA-256 of the token. Expiry 30 days from creation, refreshed on each successful authentication. Logout deletes the row and clears the cookie. Expired sessions are treated as unauthenticated and their rows deleted opportunistically by the auth middleware.

**CSRF rule:** every POST (login and register included) must carry a hidden `csrf` field matching the request's token. For authenticated pages the token is the per-session random token issued at login and re-rendered in every form. For the unauthenticated login/register pages the token is issued at GET time as a separate short-lived (1 h) `tt_csrf` cookie and mirrored in the form; on successful login both the CSRF cookie and its form value must match. Middleware rejects any POST with a missing or mismatched token: 403, no state change.

**Project name predicate (store + handler, single helper):** `name = strings.TrimSpace(raw)`; valid iff `1 <= utf8.RuneCountInString(name) <= 100` and no name equal to an existing one for this user under `COLLATE NOCASE`. Duplicate → typed error surfaced as the flash "A project with this name already exists". Empty/too-long → "Name must be 1–100 characters". Renaming is allowed on both active and retired projects; rename re-runs the same predicate excluding the project itself.

**Timer rule:** at most one running entry per user, enforced twice: domain check plus the partial unique index (last line of defense). Commands, each one transaction:
- `Start(userID, projectID)`: project must belong to the user and have `retired_at IS NULL`. If a running entry exists: same project → no-op success; different project → auto-switch: set `end_at = now` on the running entry and insert the new entry with `start_at = now` (the exact same `now` value). No running entry → insert.
- `Stop(userID)`: running entry gets `end_at = now`; none running → idempotent success.
- "Change project" in the UI is just `Start` on another project (auto-switch), matching the user-confirmed rule.

**Entry edit/delete rules:** entries with `end_at IS NULL` (running) are never editable or deletable through these endpoints — UPDATE/DELETE statements include `AND end_at IS NOT NULL` and zero affected rows maps to a typed error. Editing a closed entry may change: `project_id` (must be the user's own and active), `start_at`, `end_at`; predicate `start_at < end_at` (safe as string compare under the fixed timestamp format; still parse-and-compare in code). Entry edit forms use `datetime-local` inputs prefilled with server-local wall-clock values, each paired with a hidden offset field (`start_offset_minutes` / `end_offset_minutes`) carrying the server zone offset at that stored instant. The enhancement JS rewrites each visible value to browser-local wall clock and sets that field's paired offset to `-(new Date(data-utc).getTimezoneOffset())` — derived from the same instant being rendered — keeping each value/offset pair self-consistent. The handler converts each field: `time.Parse("2006-01-02T15:04:05", v)` then subtracts that field's offset to get UTC. With JS disabled the page edits in server-local time consistently. (Amended 2026-11-01: the originally specified single shared `tz_offset_minutes` field silently shifted stored instants by the DST delta when an entry straddled a transition or was edited from the other side of one — per-field, per-instant offsets were mandated by code review; the single-field contract is a data-loss bug, not an acceptable variant. Known residual, out of scope: offsets stay pinned to the stored instants, so a manually retyped wall time on the other side of a transition needs IANA zone names, a future feature.) Deleting an entry removes only the entry row.

**Multi-tenancy rule:** every query is keyed by the session `user_id`. Requesting a resource ID that exists but belongs to another user returns 404 (never 403; do not leak existence).

**Routes:** GET/POST `/login`, `/register`, POST `/logout`, GET `/` (single app page: timer bar, active projects, retired projects, recent entries), POST `/projects`, POST `/projects/{id}/rename`, `/projects/{id}/retire`, `/projects/{id}/unretire`, POST `/timer/start/{id}`, POST `/timer/stop`, POST `/entries/{id}/update`, POST `/entries/{id}/delete`. All state changes PRG with a one-shot flash message in the session.

**Entry list rule:** the app page shows the running entry (if any) in the timer bar plus the 100 most recent closed entries, newest first; no pagination in v1.

**Templates/assets:** `//go:embed templates/*.html web/*.css web/*.js` in `internal/web`; templates autoescape (all HTML output goes through `html/template`). Server binary stays single-file.

## Tasks

### Task 1 - Store bootstrap and migrations

- Fresh app (empty `--data-dir`) + open and migrate the DB
  - → all five tables and the `one_running_per_user` index exist
  - → running migrations a second time on the same file succeeds with no error and no duplicate data
- Same user + insert two entries with `end_at IS NULL` in separate transactions
  - → the second insert fails with a SQLite constraint error
  - → the first entry remains untouched
- Two different users + each inserts one entry with `end_at IS NULL`
  - → both inserts succeed

### Task 2 - Auth: register, login, sessions, logout

- New user + register "alice" with 12-char password
  - → user row exists with `passhash` matching the `pbkdf2$sha256$<iter>$…` format and no plaintext password anywhere in the row
- Username "Alice" registered + register "alice" again
  - → duplicate error (case-insensitive uniqueness)
- Username "  " or password "short" (7 chars) + register
  - → validation error, no user row
- Registered user + login with correct password
  - → `tt_session` cookie set, session row exists with `expires_at` 30 days ahead
- Registered user + login with wrong password
  - → error, no session row created
- Stored passhash + verify with wrong password
  - → verification returns false without panic or timing-dependent early exit
- Valid session cookie + request authed page
  - → middleware injects the session user into context
- Session row with `expires_at` in the past + request authed page
  - → treated as unauthenticated (redirect to `/login`) and the row is deleted
- Logged in + POST `/logout` with valid CSRF token
  - → session row deleted and `tt_session` cookie cleared

### Task 3 - Projects: create, rename, retire, un-retire, scoping

- User with no projects + create "Website"
  - → active project row (NULL `retired_at`)
- User has "Website" + create "website " (extra space, different case)
  - → duplicate error, exactly one row remains
- Create "  Work  "
  - → stored as "Work" (trimmed)
- Create name "" or 101 characters
  - → validation error, no row
- Active project + rename to a new unique name
  - → name updated, tracking data intact
- Retired project + rename
  - → rename succeeds
- User has "A" and "B" + rename "B" to "a"
  - → duplicate error
- Active project + retire
  - → `retired_at` set, project listed as retired
- Retired project + un-retire
  - → `retired_at` NULL, project listed as active again
- User B + read/rename/retire project owned by user A by ID
  - → not-found error (no cross-user leak)

### Task 4 - Timer: start, stop, auto-switch

- No running entry + start active project P
  - → one entry with `project_id = P`, `end_at IS NULL`
- Running entry on A + start active project B
  - → A's entry closed with `end_at = now`
  - → new running entry on B with `start_at` equal to A's `end_at`
  - → exactly one running entry remains (verified through the index)
- Running entry on A + start A again
  - → still exactly one running entry, its `start_at` unchanged
- Running entry + stop
  - → entry closed, `end_at >= start_at`
- No running entry + stop
  - → success, no-op, no new rows
- Start a retired project while a timer runs on another project
  - → domain error, the running entry is not closed, no new entry
- Two concurrent `Start` calls for the same user on different projects
  - → after both, exactly one running entry exists (one call may fail with the constraint error and must surface as a retryable conflict, not corrupt state)

### Task 5 - Time entries: list, edit, delete

- User A with entries + list as user A
  - → only user A's entries, newest first, running entry excluded from the closed list
- Closed entry + update `end_at` to 1 hour later
  - → stored `end_at` reflects the change
- Closed entry + move it to another of the user's active projects
  - → `project_id` updated
- Closed entry + move it to a retired project, or to another user's project
  - → error, entry unchanged
- Closed entry + update with `end_at <= start_at`
  - → validation error, entry unchanged
- Running entry + attempt update or delete through the entry endpoints
  - → error, the running entry is untouched
- Closed entry + delete
  - → row gone, project and user data untouched

### Task 6 - HTTP surface: routing, CSRF, escaping

- Unauthenticated GET `/`
  - → 302 to `/login`
- Logged in + POST `/projects` with valid CSRF token and name "Web site"
  - → 302 to `/`, project appears in the page body
- Logged in + POST `/projects` with valid token and duplicate name
  - → 302 to `/`, flash error rendered, no new row
- Logged in + POST `/projects` without or with wrong `csrf` field
  - → 403, no state change
- Logged in + POST `/timer/start/{id}` then `/timer/stop`
  - → running entry appears then closes; stop button and start buttons render accordingly
- Logged in + POST `/timer/start/{id}` where `id` is a project owned by another user
  - → 404, no running entry created
- User + create project named `<b>x</b>` + GET `/`
  - → body contains `&lt;b&gt;`, no raw `<b>` element from user data
- POST `/entries/{id}/update` with `start_offset_minutes=-480`, `end_offset_minutes=-420`,
  `start_at` `2026-05-01T10:00:00` and `end_at` `2026-05-01T12:00:00`
  - → stored `start_at`/`end_at` converted to UTC per field (start plus 8 hours, end plus 7 hours)
- POST `/entries/{id}/update` with a per-field offset outside ±840 (or non-numeric)
  - → 302 with an invalid-offset flash, entry unchanged; ±840 accepted
- Logged in + POST `/entries/{id}/update` and POST `/entries/{id}/delete` targeting a running entry
  - → 302 with error flash each, running row untouched and no new rows
- GET `/` then POST `/entries/{id}/update` resubmitting the rendered values and hidden offsets
  unchanged (server zone forced to a non-UTC location, incl. an entry straddling a DST change)
  - → stored `start_at`/`end_at` identical UTC instants

### Task 7 - Frontend design pass with the impeccable skill

- GET `/` (and `/login`) + parse HTML
  - → includes `<meta name="viewport" content="width=device-width, initial-scale=1">` and a single linked stylesheet served by the app
- GET `/` with a running entry
  - → running start rendered as `<time datetime="<UTC RFC3339>">` so the enhancement JS can tick elapsed time client-side; duration is also present as static server-rendered text (JS-free fallback)
- GET any app page + assert no inline event handler attributes (`onclick=` etc.)
  - → all JS lives in the single embedded file; enhancement only, every flow works without it

## Bootstrap

```sh
cd /app
make setup                                # installs golangci-lint v2.15.0 + govulncheck v1.9.0 into .bin/, enables git hooks
go get modernc.org/sqlite@v1.61.1         # the only new module dependency (see Technical Context)
go mod tidy                               # creates go.sum — commit it per AGENTS.md
make fmt && make test                     # fast inner loop
make check                                # full gate (fmt-check, lint, vet, test, tidy-check, verify, build, vuln; needs internet)
make run                                  # serves on :8080, data in ./data/timetracker.db
```

## Technical Context

- `modernc.org/sqlite v1.61.1` — original pin, restored after the deliberate go 1.26 migration below (v1.61.1's own go.mod requires `go >= 1.26`; during the 1.24 pin we ran the interim fallback v1.45.0). Pure Go (no CGO for the app), driver name `sqlite`, supports file DSNs, `_pragma` query params, and partial unique indexes; constraint violations surface as `*sqlite.Error` with SQLITE_CONSTRAINT extended codes.
- Go deliberately migrated `1.24.4 -> 1.26.10` in go.mod on 2026-11-01 (orchestrator-approved). The govulncheck gate in `make check` (also the pre-commit hook) flags reachable stdlib vulns under any 1.24.x once the mandated `net/http` + `database/sql` stack exists: GO-2026-4341/GO-2025-3849 (fixed by 1.24.13, the final 1.24 release) and GO-2026-6617 (`net/http` HPACK race under `ListenAndServe`, fixed in 1.26.9) + GO-2026-6809 (`crypto/x509`, fixed in 1.26.10). Go 1.24 is EOL, so staying current on the latest patch release of the active Go minor is required for the gate to be passable. Stdlib otherwise unchanged for us: `net/http`, `html/template`, `go:embed`, `database/sql`, `crypto/pbkdf2`, `crypto/rand`, `crypto/sha256`, `crypto/subtle`.
- `go.sum` will appear for the first time with this story; AGENTS.md requires committing it once real dependencies exist. `golangci-lint` runs with `modules-download-mode: readonly`, so keep `go.mod`/`go.sum` tidy or lint fails.
- Lint gates apply to test files too and are strict: `funlen` 80 lines / 50 statements including comments, `cyclop` complexity ≤ 12, `gocognit` ≤ 15. Split handlers and test setup into small helpers.
- The pre-commit hook runs `make check` against the staged snapshot only; stage `Makefile` and `go.mod`/`go.sum` along with code.
- No markdown formatter is configured in this repo (checked CI, Makefile, `.golangci.yml`, `.editorconfig`), so planning artifacts need no format pass; `.editorconfig` wants 2-space indent for non-Go files.
- Server flags: `-addr` (default `:8080`), `-data-dir` (default `./data`). DB file `timetracker.db` inside the data dir; directory created with `os.MkdirAll` if missing.

## Notes

- UI design must use the global impeccable skill at `/home/node/.agents/skills/impeccable/SKILL.md`. Its setup step 1 (`node .agents/skills/impeccable/scripts/context.mjs`) will print `NO_PRODUCT_MD` because this project keeps its product doc at `docs/product.md` (peck convention) instead of root `PRODUCT.md` — do NOT follow the skill's init blocker; treat `docs/product.md` as the product context instead. Then: read `reference/product.md` (this is app UI: design serves the product), run `node /home/node/.agents/skills/impeccable/scripts/palette.mjs` once for the brand seed (no committed tokens exist yet), use OKLCH tokens in the one CSS file, and obey every rule in the skill's "General rules" and "Absolute bans" sections (contrast ≥ 4.5:1, no glassmorphism, no side-stripe borders, card radius ≤ 16px, reduced-motion alternatives, verb+object button labels like "Start tracking", "Stop timer", "Retire project", "Save changes").
- Non-automatable UX requirements: mobile-first layout (one column, thumb-reachable primary actions: the running timer bar sits at the top; on desktop a wider two-pane layout is acceptable), generous tap targets (≥ 44px), running timer visibly obvious with live elapsed time ticking every second, immediate visual feedback for every action via the flash message, empty states for "no projects yet" and "no entries yet".
- Elapsed-time ticking and the per-field timezone offsets are set by the single embedded JS file; the page must remain fully correct (server-rendered values) with JS disabled.
- Time storage and conversion are UTC-only; UI displays browser-local times via the enhancement JS, falling back to server-local rendering. Clock skew within one server is not handled.
- Known security limits to record in `docs/product.md` (honesty, not blockers): no login rate limiting or lockout; HTTP without TLS is the dev default (run behind a TLS-terminating proxy in real deployments).
- Retiring a project keeps its name reserved for the user (the unique constraint spans retired projects), so un-retiring can never collide and a retired name cannot be reused by the same user.
