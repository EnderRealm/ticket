---
id: tui-epic-hierarchical-4671
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T18:18:59Z
type: task
priority: 2
parent: tui-revamp-port-0365
tags: [tui, tkt-port]
---
# TUI epic hierarchical view with progress and effort

Add a dedicated epic view to the TUI. ticket currently has no visualization for epics — you just see a `parent` field on children. tkt's epic view is one of its strongest UI features.

**What to build:**
- Enter epic view when selecting an epic ticket (Enter key)
- Epic header: title, stage badge, risk level, progress bar (X/Y children done, %)
- Description preview: first 5 lines of epic description
- Children tree listing, each child showing:
  - Cursor indicator (> prefix)
  - Stage badge (colored by pipeline stage, not just status)
  - Priority (P#)
  - Ticket ID + title
  - Inbound dependency arrows (← [dep IDs]) for deps between siblings
  - Latest commit @SHA (from commit journal)
  - Effort summary [+N -N, K files] (from commit journal)
- Dep edge summary: total count of inter-child dependencies
- Navigation: j/k scroll children, Enter opens child detail, Esc returns to board
- Sorting: by stage order (pipeline position) then priority

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Epic model | `internal/tui/epicview.go:62-93` | `EpicModel` struct with `latestByID`, `effortByID` maps |
| Render | `internal/tui/epicview.go:94-264` | `renderEpicView()` — header, progress, children, deps |
| Dep index | `internal/tui/epicview.go:265-282` | `buildDepIndex()` — maps child → inbound dep edges |
| Progress bar | `internal/tui/epicview.go:~130` | `X/Y (Z%)` format with lipgloss bar |
| Board integration | `internal/tui/app.go` | Enter on epic type → switch to epic view |

**Adaptation notes:**
- ticket has richer per-child data than tkt (stage, risk, review status) — show stage badge instead of status badge
- ticket's pipeline stages should determine the sort order, not alphabetical
- Wire into existing App model as a new view alongside dashboard/detail/pipeline/form/review
