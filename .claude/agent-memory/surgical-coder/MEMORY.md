# Surgical Coder Memory

## Project: ticket (tk CLI)
Go binary with CLI (cobra), TUI (bubbletea), and MCP server.

## Key Locations
- `pkg/ticket/` — Core types: Stage, TicketType, RiskLevel, ReviewState, Priority (int 0-4)
- `pkg/ticket/ticket.go` — Type definitions and constants
- `internal/tui/pipeline.go` — Color maps: `priorityColors`, `typeColors`, `stageColors`, `reviewColors`, `filterStyle`
- `internal/tui/form.go` — Edit/create form with picker fields (Type, Priority, Stage) and text fields
- `internal/tui/detail.go` — Read-only detail view for a ticket
- `internal/tui/dashboard.go` — Main list view with tabs
- `cmd/table.go` — CLI table output with ANSI colors (mirrors TUI colors)

## Patterns
- Color maps defined in `pipeline.go` are shared across dashboard, pipeline, form, and detail views
- Form picker fields: items colored with semantic color, selected item wrapped in `formCursorStyle.Render("[" + coloredText + "]")`
- lipgloss color strings: "1"=red, "2"=green, "3"=yellow, "4"=blue, "5"=magenta, "6"=cyan, "7"=white, "8"=gray
- Tests: `go test ./...` runs all; TUI tests in `internal/tui/form_test.go`
- Working directory for worktrees: `/Users/steve/.local/share/forge/projects/ticket/.claude/worktrees/`
