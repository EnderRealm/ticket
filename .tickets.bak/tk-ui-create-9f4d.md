---
id: tk-ui-create-9f4d
stage: done
status: closed
deps: []
links: []
created: 2026-03-01T00:43:50Z
type: bug
priority: 0
skipped: [implement, test]
---
# 'tk ui' On the create screen the title field doesn't wrap






Title field (and other text fields) in the create/edit form don't wrap or truncate — long text overflows past terminal width, making it invisible.\n\n## Root Cause\n\nEach form field renders on a single line with no width constraint. The prefix (cursor + label + space) takes 17 chars, leaving `width - 17` for text. No truncation was applied.\n\n## Fix\n\nShow rightmost portion of text when it exceeds available width (`width - 18` accounting for block cursor). Standard single-line input viewport behavior.

## Test Results

- [x] `go build ./...` — compiles clean\n- [x] `go test ./...` — all tests pass\n- [ ] Manual TUI testing required — type a long title in create form, verify it stays visible

## Review Log

**2026-03-01T01:03:52Z [human:steve]**
APPROVED — Manual TUI testing passed — cursor movement, viewport scrolling, and truncation all working
