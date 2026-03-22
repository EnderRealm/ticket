---
id: enhanced-epic-view-6351
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T04:00:53Z
type: task
priority: 3
parent: migrate-tk-storage-eb6c
tags: [views, tkt-port]
---
# Enhanced epic-view with commit rollups and effort

Enhance tk's existing epic view with commit counts, effort rollups, and dependency edges between children. tk already has `tk ls --parent <id>` — this adds operational intelligence on top.

**What to add:**
- Direct commit count per child ticket
- Rolled-up commit count (including parent-level commits)
- Effort summary per child (lines added/removed, files changed)
- Latest commit SHA per child (for quick "what happened last" reference)
- Dependency edges between children (which children depend on each other)

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Epic-view command | `internal/cli/precomputed_views.go:34-110` | `runEpicView()` — loads epic + children + journal, computes rollups |
| Commit counts | Same file, lines 71-73 | `CountJournalForTicket()` per child, separate direct vs rolled_up |
| Child dep edges | Same file, lines 61-66 | Filter dep edges where both source and target are children |
| TUI epic model | `internal/tui/epicview.go:62-93` | `EpicModel` with `latestByID` (short SHA) and `effortByID` (effort string) |
| TUI dep index | `internal/tui/epicview.go:265-282` | `buildDepIndex()` — maps child → inbound dep edges |
| Effort helper | `internal/journal/entry.go:42-58` | `Effort()` — sums LOC, deduplicates files |

**Adaptation notes:**
- tk already has parent-child relationships and dependency tracking — this is additive
- tk's TUI board view could show effort badges similar to tkt's epic view
- Wire as both CLI (`tk epic-view <id>`) and MCP tool
