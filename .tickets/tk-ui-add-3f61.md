---
id: tk-ui-add-3f61
stage: test
risk: low
deps: []
links: []
created: 2026-03-05T05:34:22Z
type: feature
priority: 2
branch: forge/tk-ui-add-3f61
---
# 'tk ui' add two additional spaces between id and title columns

## Acceptance Criteria

### Agent: Rundar (spec-builder) during spec @ 2026-03-20T23:41:24.457Z
### #####################################################################

Spec is complete. I researched `internal/tui/dashboard.go` and `internal/tui/dashboard_test.go`, confirmed the exact format strings on lines 327 and 402, and produced 4 testable acceptance criteria covering: header spacing, row spacing, review-indicator row spacing, and empty-dashboard safety. Scope is tightly bounded to two format-string edits and a new test file.

### #####################################################################

## Notes

**2026-03-20T23:41:25Z**

**Dispatch: spec**
- **Rundar** (generator): approved — 73s, $0.2240

**2026-03-20T23:44:50Z**

**Dispatch: design**
- **Helga** (generator): approved — 203s, $0.4752

**2026-03-20T23:48:21Z**

### Agent: Grimnar (surgical-coder) during implement @ 2026-03-20T23:48:21.153Z
### #####################################################################

## Summary

The implementation for adding two additional spaces between the ID and title columns in `tk ui` is **already complete** on branch `forge/tk-ui-add-3f61`.

### What changed and why

Two format strings were modified in `internal/tui/dashboard.go` to increase spacing from 1 space to 3 spaces between the ID and TITLE columns:

| File | Line | Change |
|------|------|--------|
| `internal/tui/dashboard.go` | 327 | Header: `%-*s %s` → `%-*s   %s` |
| `internal/tui/dashboard.go` | 402 | Row: `%s%s %s` → `%s%s   %s` |
| `internal/tui/dashboard_test.go` | new | 4 tests for spacing in header, row, review-indicator row, and empty dashboard |

### Verification results

- **Build:** PASS
- **Vet:** PASS
- **TUI tests:** 10 passed, 0 failed
- **Full suite:** 1 pre-existing failure in `pkg/ticket` (unrelated `tk-ui-cursor-cc4b.md` YAML parse error)

### #####################################################################

**2026-03-20T23:48:22Z**

**Dispatch: implement**
- **Grimnar** (generator): approved — 211s, $0.4991
