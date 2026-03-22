---
id: tui-commit-mutation-b525
stage: backlog
risk: normal
deps: [commit-journal-background-e2be, mutation-journal-source-6af8]
links: []
created: 2026-03-22T18:19:31Z
type: task
priority: 2
parent: tui-revamp-port-0365
tags: [tui, tkt-port, journal]
---
# TUI commit and mutation data in detail view

Add commit history, lifecycle data, effort measurement, and mutation log to the ticket detail view. Currently the detail view shows frontmatter + body + reviews + notes — this adds the operational intelligence layer.

**What to add to detail view:**

1. **Lifecycle section** (after frontmatter, before body):
   - Opened: creation timestamp
   - First commit / Last commit timestamps
   - Work started / Work ended
   - Calendar time (created → closed/now)
   - Work time (sum of commit durations)
   - Idle time (calendar minus work)
   - Format durations as human-readable (2h 15m, 3d 4h, etc.)

2. **Linked Commits section** (after body sections):
   - Last 10 commits linked to this ticket
   - Each showing: short SHA, date, author, first line of message, +N/-N diff stats
   - Close markers for commits that closed the ticket
   - Total effort summary at bottom: "+N -N, K file(s), M commits"

3. **Mutation Log section** (after commits):
   - Recent mutations from mutations.jsonl
   - Each showing: timestamp, operation, source (human/claude/codex), fields changed
   - Gives visibility into who changed what — agents vs humans
   - Last 10 entries, sorted most recent first

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Detail lifecycle | `internal/tui/detail.go:~50-120` | Lifecycle section rendering with formatted durations |
| Commit table | `internal/tui/detail.go:~120-180` | Linked commits with SHA, message, stats, close markers |
| Effort display | `internal/tui/epicview.go:72` | `effortByID` map, `EffortSummary.String()` |
| Lifecycle compute | `internal/journal/lifecycle.go:18-104` | `Lifecycle()` — timestamps, durations |
| Effort compute | `internal/journal/entry.go:42-58` | `Effort()` — LOC sums, file dedup |
| Duration format | `internal/journal/lifecycle.go:124-130` | `FormatSeconds()` |

**Adaptation notes:**
- ticket's detail view uses line-based scrolling, not viewport — the new sections just append more lines
- Mutation log is ticket-specific (tkt doesn't show it in TUI) — use the MutationEntry data from the mutation journal ticket
- Style commit SHAs and mutation sources with distinct colors for scanability
- Depends on commit-journal-background-e2be and mutation-journal-source-6af8 for data
