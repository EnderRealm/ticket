---
id: lifecycle-command-ticket-cff9
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T04:00:26Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [views, journal, tkt-port]
---
# Lifecycle command — ticket history with effort measurement

Add `tk lifecycle <id>` command that shows a ticket's complete history — when it was created, when work started, commits, effort, and total calendar/work/idle time.

**What it shows:**
- Created timestamp, first commit, last commit, closed timestamp
- Work started/ended (from commit journal)
- Calendar seconds (created → closed/now)
- Work seconds (sum of commit durations)
- Idle seconds (activity window minus work)
- Total commits, lines added/removed, files touched

**Data types:**

LifecycleSummary: Opened, FirstCommit, LastCommit, WorkStarted, WorkEnded, ClosedAt, CalendarSeconds, WorkSeconds, IdleSeconds

EffortSummary: LinesAdded, LinesRemoved, FilesChanged, Commits

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Lifecycle command | `internal/cli/precomputed_views.go:273-345` | `runLifecycle()` — loads ticket + journal, calls lifecycle.Lifecycle() |
| Lifecycle computation | `internal/journal/lifecycle.go:18-104` | `Lifecycle()` — iterates entries, finds min/max timestamps, computes durations |
| Time helpers | `internal/journal/lifecycle.go:106-130` | `parseRFC3339()`, `formatRFC3339()`, `FormatSeconds()` |
| Effort computation | `internal/journal/entry.go:42-58` | `Effort()` — sums LOC, deduplicates files, counts commits |
| Effort display | `internal/journal/entry.go:34-39` | `EffortSummary.String()` → "+N -N, K file(s)" |

**Adaptation notes:**
- tk has stage timestamps that tkt doesn't — lifecycle could show time spent in each pipeline stage, not just overall calendar/work/idle
- Consider showing stage-by-stage breakdown: "spec: 2h, design: 4h, implement: 6h, test: 1h"
