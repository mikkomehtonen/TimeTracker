# AGENTS.md

Minimal Go starter template (module `example.com/timetracker`). Single entrypoint: `cmd/timetracker/main.go`. New application code goes under `internal/` with tests in `*_test.go` next to it.

## Commands

- Use `make` targets, not raw tool invocations. Run `make help` for the list.
- Full verification is `make check` (= fmt-check, lint, vet, test, tidy-check, verify, build, vuln in that order). It needs internet access for the vulnerability check.
- Lint/format go through golangci-lint v2 installed into `.bin/` by Makefile targets; pinned versions live in `Makefile`. Never add dev tools as module dependencies.
- Single focused checks: `make lint`, `make vet`, `go test -race -run TestName ./pkg`, `make fmt` to format.
- Tests run with `-race -shuffle=on -count=1`; race detector needs CGO and a C compiler.

## Gotchas

- `go.mod` pins an exact Go patch version (`go 1.24.4`). CI reads it via `go-version-file`, and `GOTOOLCHAIN=auto` will download a newer toolchain if `go.mod` is bumped past the installed Go. Keep it in sync with the target toolchain.
- A pre-commit hook runs `make check` on a snapshot of the *staged* index only; unstaged/untracked changes do not affect the result. After fixing a failure you must `git add` the fix, and `Makefile` + `go.mod` must be staged. Run `make setup` (or `make hooks`) after cloning to enable it.
- No `go.sum` exists on purpose (zero dependencies). `golangci-lint` runs with `modules-download-mode: readonly`, so `make lint` will not create one. Add `go.sum` to git only when real dependencies are introduced.
- Module name is a placeholder. If renamed, also update `goimports` `local-prefixes` in `.golangci.yml` and all internal imports.
- Lint limits apply to test files too, and `funlen` counts comment lines: max 80 lines / 50 statements, cyclomatic ≤ 12, cognitive ≤ 15.

## Style

- `.editorconfig` is enforced: Go files use tabs; all other files 2 spaces. CI (`.github/workflows/ci.yml`) runs the same `make check` as the pre-commit hook.
