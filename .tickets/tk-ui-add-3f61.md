---
id: tk-ui-add-3f61
stage: design
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
