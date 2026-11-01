# TimeTracker

A multi-user, server-rendered web time tracker. Users register, start and stop
a timer per project, rename and retire projects, and edit or delete closed
entries. The UI is plain
`html/template` pages plus one progressive-enhancement JavaScript file and one
CSS file, both embedded into the single binary; persistence is SQLite via the
pure-Go `modernc.org/sqlite` driver (no C dependency for the app itself).

## Getting started

You need Go 1.26.10 (pinned in `go.mod`; with the default `GOTOOLCHAIN=auto`,
any recent Go works because the pinned toolchain is downloaded automatically),
GNU Make, curl, and a POSIX-compatible shell. The race detector requires CGO
support and a C compiler, such as GCC or Clang. Node.js (CI pins the 22 major
line) is optional locally: one frontend test runs the real `app.js` under Node and
self-skips when it is missing. The initial tool installation and vulnerability
checks require an internet connection.

```sh
make setup
make check
make run
```

`make run` (via `go run`) serves the app at `http://localhost:8080` and stores
data in `./data/timetracker.db`; `make build` produces `bin/timetracker`, and
both accept `-addr` and `-data-dir` flags to change those defaults. The `data/`
directory is ignored by Git.

The Go module uses the placeholder name `example.com/timetracker` because the
repository does not have a remote URL configured. Once the final URL is known,
update the module name in `go.mod`, the `goimports` local prefix in
`.golangci.yml`, and all internal application imports.

## Commands

| Command | Description |
| --- | --- |
| `make` or `make help` | List available commands. |
| `make setup` | Install development tools and enable Git hooks. |
| `make hooks` | Enable Git hooks in the current repository. |
| `make tools` | Install pinned tool versions in the project's `.bin/` directory. |
| `make fmt` | Format code with gofmt and organize imports with goimports. |
| `make fmt-check` | Check formatting without modifying files. |
| `make lint` | Run golangci-lint, including staticcheck, errcheck, and errorlint. |
| `make vet` | Run `go vet` analysis. |
| `make test` | Run tests with the race detector and randomized test order. |
| `make coverage` | Write a coverage report to `coverage/coverage.out`. |
| `make tidy` | Update module files. |
| `make tidy-check` | Check that `go mod tidy` would not make changes. |
| `make verify` | Verify the integrity of downloaded modules. |
| `make vuln` | Check for vulnerabilities with govulncheck. |
| `make build` | Build the application as `bin/timetracker`. |
| `make run` | Run the application. |
| `make check` | Run formatting checks, lint, vet, tests, module checks, a build, and vulnerability checks. |

`fmt`, `fmt-check`, `lint`, and `vuln` automatically install the tool they need.
Tool versions are pinned in `Makefile`. To upgrade a tool, change its version and
run `make tools` followed by `make check`. Each version has its own installation
directory, so changing the version selects the new tool.

[golangci-lint](https://golangci-lint.run/docs/welcome/install/local/) is installed
from an official binary release. [govulncheck](https://go.dev/doc/security/vuln/)
is installed separately, so the tools do not add dependencies to the application
module. The application has one real dependency (`modernc.org/sqlite`), so
`go.sum` is committed and kept in sync by `make tidy-check`.

## Quality checks

The following limits are configured in `.golangci.yml` and apply to application
code and test code. Values at the limit are allowed; exceeding a limit fails lint.
Function length includes comment lines.

| Check | Requirement |
| --- | --- |
| Cognitive complexity (`gocognit`) | At most 15 per function. |
| Cyclomatic complexity (`cyclop`) | At most 12 per function. |
| Function length (`funlen`) | At most 80 lines per function. |
| Statement count (`funlen`) | At most 50 statements per function. |
| Tests | All tests must pass. |
| Race detector | No data races detected. |

`make lint` enforces the complexity and function size limits. `make test` runs
all tests with `go test -race -shuffle=on -count=1 ./...`, so failing tests and
detected data races fail the command. Both commands are part of `make check`,
which is also used by the pre-commit hook and CI.

`internal/web/frontend_test.go` pins the browser timezone contract by running
the embedded `app.js` under Node with a DOM shim; it self-skips when Node is
missing, so CI installs Node 22 via `actions/setup-node` to keep those
assertions from silently disappearing.

## Git hooks

`make setup` or `make hooks` sets the repository's local `core.hooksPath` to
`.githooks/`. Run either command after each new clone, since Git does not enable
hooks automatically.

`pre-commit` runs `make check` before each commit and blocks the commit if any
check fails. The checks run on a copy of the staged content in a temporary
directory. Unstaged changes and untracked files therefore do not affect the
result. Tools are reused from the project's `.bin/` directory, and the temporary
directory is removed afterward. The hook also aborts unless `Makefile` and
`go.mod` are staged. Always stage fixes with `git add` before trying to commit
again.

The hook also runs vulnerability checks, so committing requires an internet
connection. Git's `--no-verify` option can bypass the local hook. To enforce
checks on the server as well, configure GitHub branch rules to require the CI
`Go checks` status check before merging.

## Project structure and development

`cmd/timetracker/main.go` is the entry point: it opens the SQLite database,
builds the application, and runs an `http.Server` with graceful shutdown.
Application code lives under `internal/`, with tests alongside in `*_test.go`
files:

| Package | Responsibility |
| --- | --- |
| `internal/store` | SQLite schema, migrations, and queries (timestamps are stored in a fixed-width UTC format so text order equals chronological order). |
| `internal/auth` | PBKDF2 password hashing, opaque session tokens, and cookie names and lifetimes. |
| `internal/projects` | Project create/rename/retire rules with per-user case-insensitive names. |
| `internal/timer` | Timer start/stop with automatic project switching and the one-running-entry-per-user invariant. |
| `internal/web` | HTTP handlers, session/CSRF middleware, templates, and the embedded `app.css`/`app.js` assets. |

Pages are fully server-rendered and work with JavaScript disabled; `app.js`
only enhances them (live elapsed-time ticking and browser-local clock fields).
Mutating requests are POSTs gated by a CSRF token and end in a redirect with a
one-shot flash message on success and on validation failures (transport-level
errors answer 4xx/5xx directly). Run `make fmt` and `make check` before pushing
changes.

GitHub Actions runs `make check` (and installs Node 22 for the frontend
contract test) on push, pull request, and manual dispatch events. CI uses the Go version
specified in `go.mod`; update its patch version when Go security updates are
released so the vulnerability gate stays passable. Dependabot proposes updates
to Go dependencies and GitHub Actions weekly.
