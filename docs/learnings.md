# Learnings

## Scripted edits via python str.replace can silently miss
**Date**: 2026-11-01
**Area**: workflow
**What happened**: Two bulk `python3 str.replace` edits to Go files didn't apply (gofmt had realigned const spacing, so the oldString never matched); one shipped in a commit message claiming the change, and the code reviewer caught the gap.
**Takeaway**: Guard every scripted replacement with an `assert oldString in s` (one recurrence was caught this way; the other shipped and a reviewer caught it), then grep the replacement text in the tree before `git add`; never trust a commit message the tree doesn't back up.

---

## /proc cmdline scans for background PIDs can match the tool's own shell
**Date**: 2026-11-01
**Area**: workflow
**What happened**: Killing a dev server by scanning `/proc/*/cmdline` for the port string also matched the persistent bash session whose cmdline contained the same command text — SIGTERM/SIGKILL hit the shell and the tool call hung.
**Takeaway**: When scanning /proc to kill scratch processes, exclude bash/shell processes (or match argv[0] of the built binary path), and prefer `setsid` + a saved PID file.

---

## Reviewer-mandated contract changes must amend the story in the same cycle
**Date**: 2026-11-01
**Area**: workflow
**What happened**: Code review mandated replacing the story-specified single `tz_offset_minutes` field with per-field offsets (the specified contract loses DST hours); acceptance review then Failed because it verifies ACs literally against story.md, costing a full escalate/decision/review cycle.
**Takeaway**: When a review fix changes a contract the story spells out, update the AC/approach text (with dated rationale) in the same commit, then re-run both reviewers.

---
## Page-wide asset/template changes need one table-driven test per affected route
**Date**: 2026-11-01
**Area**: testing
**What happened**: Adding a `<link rel="icon">` to all three templates, the new `TestPagesLinkFavicon` covered only `/login` and `/register`; code review Failed it (score 5) because a future edit dropping the link from `app.html` would keep the test green.
**Takeaway**: When a change touches every template, cover every affected route in one `[]struct{path string; j jar}` table test — use `tr.registerAndLogin("alice", "password1234")` (web_test.go) for the authed `/` page.

---

## SVG assets can be visually verified with rsvg-convert in this environment
**Date**: 2026-11-01
**Area**: workflow
**What happened**: Needed to confirm the new favicon reads as a stopwatch at icon sizes; `rsvg-convert` and ImageMagick `convert` are installed at /usr/bin, so rendering the SVG at 128px and 16px side-by-side proved legibility before commit.
**Takeaway**: For any SVG/image asset change, rasterize with `rsvg-convert -w 16 -h 16` and eyeball it; don't ship an icon based on geometry math alone.

---
## Browsers auto-request /favicon.ico, never /favicon.svg
**Date**: 2026-11-01
**Area**: workflow
**What happened**: Serving the favicon at the root path /favicon.svg was described as the "conventional path", but code review noted browsers without an explicit icon link only probe /favicon.ico — the SVG icon appears purely because all three templates carry <link rel="icon">.
**Takeaway**: The root SVG route is just a clean canonical URL; if a fallback for SVG-less clients is ever wanted, add a /favicon.ico asset or <link rel="alternate icon">, not another root SVG path.

---
## ServeFileFS fixes embed asset types, not logging or caching
**Date**: 2026-11-01
**Area**: architecture
**What happened**: Review caught that http.ServeFileFS logs nothing on open errors (toHTTPError maps them to silent 404/500 responses) and embed.FS entries report zero ModTime, so ServeContent emits no ETag/Last-Modified and no 304s; its wins were correct extension-derived types, Content-Length, and Range.
**Takeaway**: For embedded assets, keep one assetRoutes table validated at boot with fs.Stat (panics like template.Must — note fs.Stat needs the un-slashed embed spelling while ServeFileFS needs the leading-slash one) plus a table test asserting 200 + Content-Type for every served route; caching would need explicit Cache-Control or a content-hash URL.

---
