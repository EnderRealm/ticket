---
id: tk-ui-enable-b94e
stage: done
status: in_progress
deps: []
links: []
created: 2026-03-01T00:23:16Z
type: feature
priority: 1
skipped: [spec, design, implement]
---
# 'tk ui' enable edit mode leveraging the create screen








Allow editing of all fields except ID. Include editing for notes.

## Test Results

- [x] `go build ./...` — compiles clean\n- [x] `go test ./...` — all tests pass (mcp, pkg/ticket)\n- [ ] Manual TUI testing required — TUI cannot be tested programmatically (requires TTY)

## Review Log

**2026-03-01T00:47:49Z [human:steve]**
APPROVED — Manual TUI testing passed

## Notes

**2026-03-01T00:33:38Z**

## Implementation\n\n**Changes:**\n- `form.go`: Added `editID` field, `fieldNote` constant, `newEditFormModel()`, `extractDescription()`, dynamic field count (edit shows Note field, create doesn't)\n- `tui.go`: Added `e` key in dashboard + detail views, `o` alias for enter, `handleEditTicket()` that updates title/type/priority/assignee/description and optionally adds notes\n- `detail.go`: Updated help bar to consistent `(k)ey` format with `│` separators\n- `CHANGELOG.md`: Added edit mode entries\n\n**Design decisions:**\n- Reused existing form infrastructure — edit mode detected by non-empty `editID` (auto)\n- Description extracted from body text before first `## ` heading, updated via `ticket.UpdateSection` (auto)\n- Note field only shown in edit mode since new tickets have no notes (auto)\n- Edit from detail view returns to detail view; edit from dashboard returns to dashboard (auto)
