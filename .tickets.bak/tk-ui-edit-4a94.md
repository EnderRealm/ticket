---
id: tk-ui-edit-4a94
stage: done
status: in_progress
deps: []
links: []
created: 2026-03-01T00:45:11Z
type: bug
priority: 1
skipped: [implement, test]
---
# 'tk ui' edit should enable changing of workflow stage





Edit form doesn't allow changing the workflow stage. Added a Stage selector field (left/right/enter to cycle) that shows only valid stages for the ticket's type. Edit-only field, not shown in create mode.

## Test Results

- [x] `go build ./...` — compiles clean\n- [x] `go test ./...` — all tests pass\n- [ ] Manual TUI testing required — press e on a ticket, tab to Stage, cycle with arrows

## Review Log

**2026-03-01T02:32:23Z [human:steve]**
APPROVED — Manual TUI testing passed
