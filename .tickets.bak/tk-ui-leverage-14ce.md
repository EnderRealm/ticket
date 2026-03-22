---
id: tk-ui-leverage-14ce
stage: done
status: in_progress
deps: []
links: []
created: 2026-02-28T20:20:53Z
type: feature
priority: 2
skipped: [spec, design, implement, test]
---
# 'tk ui' leverage file watch to update in real-time?





TUI doesn't reflect changes made by other processes (MCP agents, CLI in another terminal). Add filesystem watching to auto-reload tickets when .tickets/ directory changes.

## Test Results

- [x] `go build ./...` — compiles clean\n- [x] `go test ./...` — all tests pass\n- [ ] Manual TUI testing required — open tk ui, edit a ticket from another terminal, verify TUI updates

## Review Log

**2026-03-01T01:14:40Z [human:steve]**
APPROVED — Manual testing passed — TUI updates in real-time from external changes
