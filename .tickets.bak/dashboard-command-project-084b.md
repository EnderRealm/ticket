---
id: dashboard-command-project-084b
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T04:00:06Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [views, tkt-port]
---
# Dashboard command — project health overview

Add `tk dashboard` command that synthesizes ticket state + commit history into a single project health view. Answers "what's happening right now?" in one command.

**What it shows:**
- Status summary: total, open, in_progress, needs_testing, closed, ready, blocked counts
- In-progress tickets (list with title, assignee, stage)
- Blocked tickets (with what they're blocked on)
- Ready tickets (unblocked, actionable)
- Recent commits linked to tickets (last 5)

**Output modes:** human-readable table (default) and `--json` with envelope

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Dashboard command | `internal/cli/precomputed_views.go:200-271` | `runDashboard()` — loads all tickets + journal, computes summary |
| Status counting | Same file, lines 235-246 | Iterates records, counts by status |
| Ready computation | Same file, lines 223-227 | Open + no open deps + (no parent or parent in_progress) |
| Blocked computation | Same file, lines 219-220 | Open + at least one open dep |
| Recent commits | Same file, line 233 | `LastNJournalEntries(entries, 5)` |

**Adaptation notes:**
- tk has richer status model (stages, not just statuses) — dashboard should show by stage, not just status
- tk has `ready` and `blocked` commands already — dashboard reuses that logic
- tk has `inbox` which computes next-action — consider including inbox summary in dashboard
- Add stage distribution (how many tickets in each pipeline stage) as a tk-specific enhancement
