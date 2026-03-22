---
id: tk-ui-create-bdc9
stage: done
status: closed
deps: []
links: []
created: 2026-02-28T16:57:20Z
type: bug
priority: 0
assignee: Steve Macbeth
---
# 'tk ui' Create task fails on id creation










## Root Cause

`handleCreateTicket` in `internal/tui/tui.go` constructs a `ticket.Ticket` without setting the `ID` or `Created` fields. The `store.Create()` method calls `Validate()` which requires a non-empty ID, so every TUI create attempt fails with `"error: create: ticket ID is required"`.

Both the CLI (`cmd/create.go`) and MCP server (`internal/mcp/mcp.go`) correctly call `ticket.GenerateID(title)` before saving — the TUI was the only caller that missed it.

## Fix

Added `ticket.GenerateID(msg.title)` and `time.Now().UTC()` to the Ticket struct literal in `handleCreateTicket` (internal/tui/tui.go:485-492), matching the pattern used by CLI and MCP create paths.

## Test Results

All tests pass (`go test ./...`). Fix verified by code inspection — the change adds `ticket.GenerateID(msg.title)` and `time.Now().UTC()` to the TUI create path, matching CLI and MCP create flows.

## Review Log

**2026-02-28T17:01:29Z [agent:ghost]**
APPROVED — One-line fix: added missing GenerateID() and Created timestamp to TUI create path, matching existing CLI and MCP patterns. All tests pass.

**2026-02-28T19:33:37Z [human:steve]**
APPROVED — Verified: all three create paths (CLI, MCP, TUI) now call GenerateID(). Tests pass. Fix is minimal and correct.
