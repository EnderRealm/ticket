---
id: tk-ui-verify-6163
stage: done
status: in_progress
review: approved
deps: []
links: []
created: 2026-03-01T00:47:17Z
type: feature
priority: 0
skipped: [spec, design, implement, test]
---
# 'tk ui' on the verify tab there should be an easy (v)erify command to verify and move to the next stage






On the verify tab, pressing v should advance the selected ticket to the next stage. Also added R on review tab to approve review.

## Test Results

- [x] `go build ./...` — compiles clean\n- [x] `go test ./...` — all tests pass\n- [ ] Manual TUI testing required — tab to verify, press v on a ticket at verify stage

## Review Log

**2026-03-01T01:30:05Z [human:tui]**
APPROVED — verified via TUI

**2026-03-01T02:17:52Z [human:steve]**
APPROVED — Manual TUI testing passed
