---
id: drop-status-ticket-6524
stage: done
status: open
deps: []
links: []
created: 2026-03-01T05:16:59Z
type: bug
priority: 2
---
# Drop status from ticket

Remove the `status` field from ticket logic. Keep it in the struct for YAML parse compat (old tickets), but zero it after read so it's never written back. Auto-migrate status-only tickets to stage on read. Remove all status-based filtering, sorting, propagation, CLI commands (start, close, reopen, status), and MCP/TUI status handling.

## Test Results

- [x] `go test ./pkg/ticket/` — all pass (store, format, filter, deps, workflow, migrate, move, pipeline, gates)
- [x] `go test ./internal/mcp/` — all pass (create, edit, show, advance, revert, notes, review)
- [x] `go test ./internal/tui/` — all pass
- [x] `go build ./...` — clean, no warnings
- [x] Legacy ticket auto-migration: status-only YAML parses and auto-migrates to stage
- [x] Serialize no longer writes status field
- [x] MigrateTicket clears status after setting stage

## Review Log

**2026-03-12T03:50:54Z [agent:impl-reviewer]**
APPROVED — Full status removal verified. All tests pass across pkg/ticket, internal/mcp, internal/tui. Backward compat preserved via auto-migration in Parse.

**2026-03-12T03:51:05Z [agent:code-reviewer]**
APPROVED — Status field fully removed from write paths, CLI, MCP, and TUI. All references replaced with stage equivalents. No dead code left behind.

**2026-03-12T03:59:06Z [human:steve]**
APPROVED — Verified

## Notes

**2026-03-12T03:50:39Z**

## Implementation Complete

**Scope of changes:**

**Removed:**
- `cmd/status.go` (start, close, reopen commands)
- `--status` filter flag from `tk ls`, replaced with `--stage`
- `status` group-by option from `tk ls --group-by`
- `status` field from MCP `ticket_create`, `ticket_edit`, `ticket_show` responses
- `ValidateStatus`, `SetStatus`, `PropagateStatus`, `StatusChange` from core library
- `SortByStatusPriorityID` renamed to `SortByStagePriorityID`
- `ListOptions.Status` replaced with `ListOptions.Stage`
- `DepNode.Status` replaced with `DepNode.Stage`
- Status display from TUI detail view
- `StatusToStage` fallback from TUI pipeline and inbox

**Changed:**
- `Validate()` now requires `Stage` (not status-or-stage dual mode)
- `Serialize()` no longer writes status field
- `Parse()` auto-migrates legacy status-only tickets to stage on read
- `MigrateTicket()` clears status after setting stage
- All `StatusClosed` checks replaced with `StageDone` checks
- `IsReady`/`IsBlocked`/`BlockingDeps`/`FindCycles` all use stage-based logic
- `move.go` no longer sets status on source or destination tickets

**Backward compat:**
- Status struct field kept for YAML parse compatibility
- Auto-migration in Parse ensures legacy tickets work seamlessly
- `tk migrate` still available for explicit bulk migration

**Tests:** All 3 packages pass (pkg/ticket, internal/mcp, internal/tui)
