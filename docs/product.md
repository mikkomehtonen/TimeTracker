# TimeTracker

TimeTracker is a multi-user time tracking app: people log work hours against their own projects, one running timer at a time per user. It runs as a single Go server binary with a responsive web UI, designed mobile-first but fully usable on desktop. It solves the "where did my day go" problem without spreadsheets or accounts tied to a third party.

## Features

- **Time tracker v1** — projects (create, rename, retire, un-retire), per-user start/stop/auto-switch timers, entry list with edit/delete, username+password login ([story](../stories/001-time-tracker/story.md))

## Non-Goals

- Reports, totals, or charts over tracked time (planned as a later story).
- Notes on time entries; entry bulk operations.
- Deleting projects; retirement is the intentional lifecycle instead.
- Account recovery, email, OAuth, 2FA.
- Native mobile apps and offline use; the app always talks to the server.
- Multi-device live sync (two devices see each other's timer state on reload, not pushed).

## Known Limitations

- v1 ships a single app page listing only the 100 most recent closed entries; no pagination or search.
- No login rate limiting or account lockout; deploy behind a TLS-terminating proxy (the app itself serves plain HTTP by default).
- Timestamps are stored in server UTC; entry edit forms interpret wall-clock input using a JS-supplied timezone offset and fall back to server-local time when JS is disabled.
