---
<<<<<<< HEAD
7stage: implement
=======
id: tk-ui-cursor-cc4b
stage: done
>>>>>>> 28a8ce1 (Added tickets)
risk: low
deps: []
links: []
created: 2026-03-01T01:14:24Z
type: bug
priority: 1
branch: forge/tk-ui-cursor-cc4b
run_id: orch-mmsopm4p-u89yid
---
# 'tk ui' cursor highlighting on Priority is messed up

The cursor is one yellow bar and one white bar. Same for Stage cursor. But the Type cursor is two yellow bars. Should be consistent. Also not sure colors of priorites and stages match color choices on the dashboard. And types don't have colors. Just in general colors on the edit/view page are kind of messed up.

## Review Log

**2026-03-16T05:35:23Z [human:tui]**
APPROVED — Looks good

## Notes

**2026-03-15T21:04:13Z**

## Triage

**Risk:** low — Purely cosmetic fix isolated to internal/tui/ styling code. No data, API, or behavior changes.

**Scope:** Single task with three related sub-issues:
1. Inconsistent cursor bar colors (yellow+white on Priority/Stage vs yellow+yellow on Type)
2. Priority/stage colors may not match dashboard color choices
3. Types lack colors entirely

**Key decisions:**
- Risk level: low (human)

**2026-03-16T04:32:38Z**

### Agent: Grimnar (surgical-coder) during implement @ 2026-03-16T04:32:38.467Z
### #####################################################################

## Summary

### What changed and why

Fixed three related color issues in the TUI edit/view pages:

**1. Inconsistent cursor highlighting (form.go)**
- **Before:** Type picker showed "two yellow bars" (brackets + text all yellow) while Priority and Stage showed "one yellow + one colored bar" (yellow brackets, colored text inside)
- **After:** Type picker options now use `typeColors` for semantic coloring, matching the pattern used by Priority (`priorityColors`) and Stage (`stageColors`). All three pickers are now consistent: yellow `[ ]` brackets with semantically-colored text inside.

**2. Types lacked colors (form.go)**
- Type values in the form now display with their dashboard colors: bug=red, feature=green, task=blue, epic=magenta, chore=gray.

**3. Detail view values lacked colors (detail.go)**
- Stage, Review, Type, and Priority values now render with their semantic colors using the same color maps used by the dashboard and pipeline views.

### Files modified
- `internal/tui/form.go` — Line 265: Added `typeColors` styling to type picker options
- `internal/tui/detail.go` — Lines 306-319: Added semantic colors to Stage, Review, Type, and Priority in detail view

### Verification
- **Build:** PASS
- **Tests:** PASS (all 3 packages, including 6 TUI-specific tests)

### #####################################################################

**2026-03-16T04:32:39Z**

**Dispatch: implement**
- **Grimnar** (generator): approved — 176s, $0.9593

**2026-03-16T04:32:43Z**

### Agent: Orchestrator (orchestrator) during verify @ 2026-03-16T04:32:43.102Z
### #####################################################################

PR created: https://github.com/EnderRealm/ticket/pull/7

### #####################################################################

**2026-03-16T04:32:43Z**

## Awaiting Human Input

**Stage:** verify
**Action needed:** Stage verify requires human review
**PR:** https://github.com/EnderRealm/ticket/pull/7
**Run:** orch-mmsopm4p-u89yid
