# AGENTS.md

Minimal Go starter template (module `example.com/timetracker`). Single entrypoint: `cmd/timetracker/main.go`. New application code goes under `internal/` with tests in `*_test.go` next to it.

## Commands

- Use `make` targets, not raw tool invocations. Run `make help` for the list.
- Full verification is `make check` (= fmt-check, lint, vet, test, tidy-check, verify, build, vuln in that order). It needs internet access for the vulnerability check.
- Lint/format go through golangci-lint v2 installed into `.bin/` by Makefile targets; pinned versions live in `Makefile`. Never add dev tools as module dependencies.
- Single focused checks: `make lint`, `make vet`, `go test -race -run TestName ./pkg`, `make fmt` to format.
- Tests run with `-race -shuffle=on -count=1`; race detector needs CGO and a C compiler.

## Gotchas

- `go.mod` pins an exact Go patch version (`go 1.26.10`; deliberately migrated from 1.24.4 — the 1.24 line is EOL and govulncheck cannot pass its `net/http`/`crypto/x509` advisories on it; see story 001 Technical Context). CI reads it via `go-version-file`, and `GOTOOLCHAIN=auto` will download a newer toolchain if `go.mod` is bumped past the installed Go. Keep it in sync with the target toolchain, and track the latest patch release of the active Go minor so the vuln gate stays passable.
- A pre-commit hook runs `make check` on a snapshot of the *staged* index only; unstaged/untracked changes do not affect the result. After fixing a failure you must `git add` the fix, and `Makefile` + `go.mod` must be staged. Run `make setup` (or `make hooks`) after cloning to enable it.
- One real dependency exists (`modernc.org/sqlite`), so `go.sum` is committed and must stay in sync. `golangci-lint` runs with `modules-download-mode: readonly`, so `make lint` will not create or fix one; run `make tidy` after dependency changes and `make tidy-check` verifies.
- Module name is a placeholder. If renamed, also update `goimports` `local-prefixes` in `.golangci.yml` and all internal imports.
- `internal/web/frontend_test.go` runs the real `app.js` under Node (the JS timezone-contract test). It self-skips locally when Node is missing; CI pins Node via `actions/setup-node` in `.github/workflows/ci.yml`, so install Node locally if you want those assertions to run.
- Lint limits apply to test files too, and `funlen` counts comment lines: max 80 lines / 50 statements, cyclomatic ≤ 12, cognitive ≤ 15.

## Style

- `.editorconfig` is enforced: Go files use tabs; all other files 2 spaces. CI (`.github/workflows/ci.yml`) runs the same `make check` as the pre-commit hook, plus `make docker` (image build; requires a Docker daemon and is deliberately not part of `check` or the hook).
