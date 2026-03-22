---
id: redesign-tui-dashboard-d727
stage: done
risk: low
deps: []
links: []
created: 2026-03-14T18:32:59Z
type: feature
priority: 2
tags: [tui, dashboard]
skipped: [spec, design]
---
# Redesign TUI dashboard tabs for backlog workflow

Reorganize the TUI dashboard tab structure to support the backlog stage and provide a more useful default view.

Current tabs: all | triage | verify | review
Proposed tabs: backlog | triage | inbox | done | all

Key changes:
- Default tab should be triage (not all) — triage is where active work decisions happen
- Add backlog tab to view the idea queue
- Replace verify/review tabs with inbox (human attention needed) and done (recently completed)
- Touches inboxTab enum, tabLabels, buildItems filtering, and default selection in dashboard.go
