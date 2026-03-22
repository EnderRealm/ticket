---
id: progress-command-changed-c4ce
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T04:00:15Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [views, tkt-port]
---
# Progress command — what changed today/this week

Add `tk progress [--today|--week]` command that shows what happened in a time window. Answers "what did I get done?" for standups and retrospectives.

**What it shows:**
- Tickets closed in the window
- Commit links (which commits touched which tickets)
- Stage transitions (ticket X moved from design to implement)

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Progress command | `internal/cli/precomputed_views.go:112-198` | `runProgress()` — window filter, journal scan, status change detection |
| Window computation | `internal/engine/helpers.go:223-230` | `WindowStart()` — midnight UTC for "today", 7 days back for "week" |
| Journal filtering | Same as progress command, lines 148-155 | Filter entries by `TS > windowStart` |
| Close detection | Same file, lines 157-171 | Entries with `Action == "close"` OR record closed with ModTime in window |

**Adaptation notes:**
- tk tracks stage transitions, not just status changes — progress should show stage movements (e.g., "design → implement") not just open/closed
- Consider showing review activity in the window too (approvals/rejections)
- tk's pipeline gives richer progress signal than tkt's flat statuses
