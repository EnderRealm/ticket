---
id: tui-cross-project-2d20
stage: backlog
risk: high
deps: [central-store-configuration-5b5b]
links: []
created: 2026-03-22T18:19:45Z
type: task
priority: 2
parent: tui-revamp-port-0365
tags: [tui, tkt-port, multi-project]
---
# TUI cross-project navigation with default inbox view

Add cross-project support to the TUI. With central store, ticket manages multiple projects — the TUI should surface all of them. The default view should be a cross-project inbox showing what needs attention across everything, with per-project views as the secondary drill-down.

**What to build:**

1. **Cross-project inbox (default view on launch):**
   - Loads tickets from ALL registered projects in `~/.tk/config.yaml`
   - Shows human-actionable items across all projects: reviews pending, human input needed, blocked tickets
   - Each row prefixed with project name for disambiguation
   - Sorted by priority then age (most urgent first)
   - Uses ticket's existing NextAction/inbox logic, just aggregated
   - Tab to switch to per-project view

2. **Project picker overlay** (`o` key):
   - Lists all registered projects from config
   - Shows project name + ticket counts (open / total)
   - Navigate with j/k, Enter selects
   - Switches to per-project view (dashboard/pipeline) for selected project
   - Esc returns to cross-project inbox

3. **Per-project views:**
   - Once a project is selected, existing dashboard/pipeline/detail/form/review views work as today
   - Project name shown in status bar
   - `o` key returns to project picker, Esc returns to cross-project inbox

4. **Status bar:**
   - Cross-project inbox: "inbox (all projects) — N items need attention"
   - Per-project: "project-name — dashboard/pipeline/detail"

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Project picker | `internal/tui/app.go` | `o` key triggers project picker overlay |
| Project list | `internal/tui/app.go` | Reads registered projects, shows with counts |
| Switch action | `internal/tui/app.go` | Sets SwitchTo flag, CLI restarts with new project |

**Adaptation notes:**
- tkt's project switching restarts the CLI process — ticket should do this in-process by reloading the FileStore with a new tickets directory
- Cross-project inbox is NEW (not in tkt) — tkt only switches between single-project views
- Central store makes this natural: all projects under `~/.tickets/`, just scan subdirectories
- Depends on central-store-configuration-5b5b for the config registry
