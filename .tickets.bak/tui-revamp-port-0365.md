---
id: tui-revamp-port-0365
stage: backlog
risk: normal
deps: [migrate-tk-storage-eb6c]
links: []
created: 2026-03-22T18:18:42Z
type: epic
priority: 2
tags: [tui, tkt-port, ux]
---
# TUI revamp — port tkt UI capabilities to ticket

Port the best of tkt's TUI features into ticket's existing Bubble Tea interface. ticket already has strong pipeline/review/form UX — this adds operational intelligence surfaces, structured filtering, cross-project navigation, and richer data density.

**What we're porting:**
1. Epic hierarchical view with progress, dep arrows, effort, and commit data
2. Structured multi-dimensional filter system with contextual picker
3. Commit + mutation data in detail view (lifecycle, effort, linked commits, mutation log)
4. Cross-project navigation with default cross-project inbox view
5. Hybrid refresh (fsnotify + periodic poll for central store)
6. Preview pane on board/pipeline view

**What we're keeping from ticket (not changing):**
- Review workflow (approve/reject with notes + stage picker)
- Pipeline kanban with advance/skip/force
- Multi-field inline form
- Move between repos
- Tab-based inbox filtering

**Source TUI code:** `~/code/tkt/internal/tui/` (app.go, board.go, detail.go, epicview.go, filter.go, keys.go, actions.go, styles.go)
**Target TUI code:** `~/code/ticket/internal/tui/` (tui.go, dashboard.go, detail.go, pipeline.go, form.go, review.go, watcher.go)

**Depends on:** The operational intelligence epic (migrate-tk-storage-eb6c) for commit journal, mutation journal, and central store — several of these TUI features surface data from those systems.
