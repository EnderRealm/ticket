---
id: tk-ui-remove-d7e3
status: closed
review: approved
deps: []
links: []
created: 2026-02-28T19:48:43Z
type: feature
priority: 0
---
# 'tk ui' remove status view and make pipeline view the default






## Test Results

`go test ./...` passes (no TUI-specific tests exist). Build compiles clean. **Manual testing required:** pipeline as default view, `/` text search, `p` priority cycling, `c` create, `esc` from detail back to pipeline, form cancel back to pipeline.

## Review Log

**2026-02-28T20:08:14Z [agent:ghost]**
APPROVED — Deleted list.go, removed viewList/listModel/cycleStatus/statusCycle. Pipeline is now default with text search (/) and priority cycling (p). Detail esc returns to pipeline. Form cancel returns to pipeline.

## Notes

**2026-02-28T19:51:50Z**

Moved from tk-ui-remove-deaf in /Users/steve/code/forge
