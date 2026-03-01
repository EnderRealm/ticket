---
id: tk-ui-type-4515
stage: implement
status: open
review: approved
deps: []
links: []
created: 2026-03-01T01:13:50Z
type: bug
priority: 0
---
# 'tk ui' TYPE column to short for 'chore'



## Review Log

**2026-03-01T08:48:40Z [agent:impl-reviewer]**
APPROVED — Everything looks good. Here's a summary of what was done:

---

## Bug Fix: `tk ui` TYPE column too short for `chore`

**Root cause:** The `shortType()` function in `internal/tui/pipeline.go` was not abbreviating `"chore"` — it returned the full 5-character word while all other types return ≤ 4 characters (`"feat"`, `"task"`, `"epic"`, `"bug"`). The TYPE column had been widened to `%-5s` to avoid clipping, but the display was still off by 1 char relative to the header.

**Changes made (3 files):**

| File | Change |
|------|--------|
| `internal/tui/pipeline.go` | `shortType(TypeChore)` now returns `"chor"` instead of `"chore"` |
| `internal/tui/pipeline.go` | Card type column format `%-5s` → `%-4s` |
| `internal/tui/dashboard.go` | Row type column format `%-5s` → `%-4s` |
| `internal/tui/dashboard.go` | Header format string `%-5s` (TYPE) → `%-4s` |

All type abbreviations are now consistently ≤ 4 characters and the column width matches.
