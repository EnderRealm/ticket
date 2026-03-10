# Spec Builder Memory

## Project Conventions

- Version string lives in `cmd/root.go` as `var Version = "dev"` (set via ldflags). The `version()` function formats it. TUI lives in `internal/tui/` and has no access to `cmd/` — version must be passed in.
- TUI entry point: `tui.New(ticketsDir string) App` called from `cmd/ui.go`.
- `App.View()` in `tui.go` is the render root — prepend global header there.
- Child models (dashboard, pipeline) track their own height. When a header row is added to `App.View()`, child models need height reduced by the header height to avoid overflow. Pass `a.height - headerLines` to `setSize()` calls in `WindowSizeMsg` handler.
- `dashboardModel.visibleRows()` reserves `height - 3`. `pipelineModel.view()` uses `height - 2` for `availH`. Both derive from `m.height` set via `setSize()`.
- Ticket counts: iterate `a.tickets []*ticket.Ticket`, bucket by `t.Stage`. `StageDone = "done"` is the closed/terminal stage. Stages: triage, spec, design, implement, test, verify, done.
- `a.ticketsDir` holds the `.tickets/` path. Parent repo dir = `filepath.Dir(a.ticketsDir)`.

## Testing Patterns

- TUI has no dedicated unit tests currently. Functional verification is manual (`tk ui`).
- Go tests: `go test ./...` from repo root.
