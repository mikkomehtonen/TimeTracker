# TimeTracker

A lightweight Go project starter with a runnable command-line application and
preconfigured development tools. The application does not yet include business
logic or tests.

## Getting started

You need Go 1.26.8 or later, GNU Make, curl, and a POSIX-compatible shell.
The race detector requires CGO support and a C compiler, such as GCC or Clang.
The initial tool installation and vulnerability checks require an internet connection.

```sh
make setup
make check
make run
```

The Go module uses the placeholder name `example.com/timetracker` because the
repository does not have a remote URL configured. Once the final URL is known,
update the module name in `go.mod` and the local import prefix in `.golangci.yml`.
Update any internal application imports as well.

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
module. The application has no external dependencies yet, so `go.sum` is
intentionally absent. Add it to version control when you introduce dependencies.

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

## Git hooks

`make setup` or `make hooks` sets the repository's local `core.hooksPath` to
`.githooks/`. Run either command after each new clone, since Git does not enable
hooks automatically.

`pre-commit` runs `make check` before each commit and blocks the commit if any
check fails. The checks run on a copy of the staged content in a temporary
directory. Unstaged changes and untracked files therefore do not affect the
result. Tools are reused from the project's `.bin/` directory, and the temporary
directory is removed afterward. Always stage fixes with `git add` before trying
to commit again.

The hook also runs vulnerability checks, so committing requires an internet
connection. Git's `--no-verify` option can bypass the local hook. To enforce
checks on the server as well, configure GitHub branch rules to require the CI
`Go checks` status check before merging.

## Project structure and development

`cmd/timetracker/main.go` is the application's entry point. Add application
packages under `internal/` as needed, with tests alongside them in `*_test.go`
files. Run `make fmt` and `make check` before pushing changes.

GitHub Actions runs `make check` on push and pull request events. CI uses the Go
version specified in `go.mod`. Update its patch version when Go security updates
are released. Dependabot proposes updates to Go dependencies and GitHub Actions
weekly.
