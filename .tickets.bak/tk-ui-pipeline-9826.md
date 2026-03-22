---
id: tk-ui-pipeline-9826
status: closed
review: approved
deps: []
links: []
created: 2026-02-28T19:48:19Z
type: bug
priority: 0
---
# 'tk ui' Pipeline view should support 'c' for create






## Test Results

`go test ./...` passes (no TUI-specific tests exist). Build compiles clean. **Manual testing required:** press `c` in pipeline view to confirm create form opens.

## Review Log

**2026-02-28T20:01:09Z [agent:ghost]**
APPROVED — Added 'c' case to viewPipeline key handler, reusing existing newFormModel/viewForm transition. Updated help bar to show 'c create'.

## Notes

**2026-02-28T19:51:31Z**

Moved from tk-ui-pipeline-d0c2 in /Users/steve/code/forge
