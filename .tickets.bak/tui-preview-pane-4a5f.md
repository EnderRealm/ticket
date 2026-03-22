---
id: tui-preview-pane-4a5f
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T18:20:06Z
type: task
priority: 3
parent: tui-revamp-port-0365
tags: [tui, tkt-port]
---
# TUI preview pane on pipeline view

Add a preview pane at the bottom of the pipeline and dashboard views showing details of the selected ticket. Currently you must press Enter to see anything beyond ID/title/priority — the preview pane gives instant context without leaving the board.

**What to show (5-line preview):**
- Line 1: ID | Stage | Type | Priority | Risk | Assignee
- Line 2: Title (full, may truncate)
- Line 3: Dependencies (if any) — "Deps: [id1] (done), [id2] (implement)"
- Line 4: Latest commit (if any) — "@abc1234 Fix the thing (+15 -3)"
- Line 5: Review status (if any) — "Review: approved by human:steve" or "Review: pending"

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Preview pane | `internal/tui/board.go:~200-260` | 5-line preview section below columns |
| Preview content | `internal/tui/board.go` | ID, status, type, priority, assignee, parent, deps, title, latest commit |
| Layout | `internal/tui/board.go` | Terminal height minus preview height = column height |

**Adaptation notes:**
- ticket's pipeline view uses full terminal height for columns — reserve bottom 5-7 lines for preview
- Dashboard list view should also show preview (same data, same position)
- Include review status and risk level (ticket-specific, not in tkt preview)
- Latest commit line depends on commit journal being available — show "No commits linked" if journal empty
