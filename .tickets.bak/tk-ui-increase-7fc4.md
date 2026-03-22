---
id: tk-ui-increase-7fc4
status: closed
review: approved
deps: []
links: []
created: 2026-02-28T19:45:27Z
type: bug
priority: 1
assignee: Steve Macbeth
---
# 'tk ui' increase width of id column to support longer ids






## Test Results

`go test ./...` passes (no TUI-specific tests exist). Build compiles clean. **Manual testing required:** verify ID column width adjusts to longest ID in `tk ui` list view.

## Review Log

**2026-02-28T19:58:08Z [agent:ghost]**
APPROVED — 3-line fix: replaced hardcoded %-9s with dynamic %-*s computed from max ID length in filtered tickets. Header and rows stay aligned.

## Notes

**2026-02-28T19:51:06Z**

Moved from tk-ui-increase-57c4 in /Users/steve/code/forge
