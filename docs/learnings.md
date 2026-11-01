# Learnings

## Scripted edits via python str.replace can silently miss
**Date**: 2026-11-01
**Area**: workflow
**What happened**: Two bulk `python3 str.replace` edits to Go files didn't apply (gofmt had realigned const spacing, so the oldString never matched); one shipped in a commit message claiming the change, and the code reviewer caught the gap.
**Takeaway**: After any scripted edit, grep for the replacement text (or assert in the script itself) before `git add`; never trust a commit message the tree doesn't back up.

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
