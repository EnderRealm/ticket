---
id: commit-journal-background-e2be
stage: backlog
risk: high
deps: []
links: []
created: 2026-03-22T03:59:18Z
type: task
priority: 1
parent: migrate-tk-storage-eb6c
tags: [architecture, journal, tkt-port]
---
# Commit journal and background daemon

Add a background daemon that watches git log and links commits to tickets. This is tkt's most distinctive feature — it makes code history visible inside the ticket system.

**What to build:**

1. **CommitJournalEntry data type** — SHA, ticket ID, repo, timestamp, message, author, action (ref/close), files changed, lines added/removed, branch, work_started, work_ended, duration_seconds

2. **Git log watcher** — polls git log on interval, extracts `[ticket-id]` bracket refs and `Closes: [id]`/`Fixes: [id]` keywords from commit messages, computes diff stats via `git diff-tree --numstat`

3. **JSONL journal** — append-only `commits.jsonl` at `~/.tk/state/<project>/commits.jsonl`, one entry per line, deduplicates by SHA

4. **Daemon lifecycle** — `tk serve start|stop|status|logs` commands with PID file at `~/.tk/state/serve.pid`, log file at `~/.tk/state/serve.log`

5. **Journal reading API** — ReadJournalEntries, FilterJournalByTickets, CountJournalForTicket, LastNJournalEntries, GroupByTicket

6. **Recompute** — `tk recompute` rebuilds commits.jsonl from scratch by scanning full git history

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Entry type | `internal/engine/types.go:6-21` | `CommitJournalEntry` struct |
| Journal paths | `internal/engine/paths.go:10-16` | `JournalPath()` → `~/.tkt/state/<project>/commits.jsonl` |
| Git log collection | `internal/cli/watch_commands.go:589-626` | `collectCommitsForWatch()` — `git log --reverse --pretty=format` |
| Ticket extraction | `internal/cli/watch_commands.go:628-663` | `extractTicketActions()`, `extractBracketList()` — regex for `[id]` and `Closes:`/`Fixes:` |
| Diff stats | `internal/cli/migrate_recompute_commands.go:347-371` | `getDiffStats()` — `git diff-tree --numstat` |
| Watch cycle | `internal/cli/watch_commands.go:411-551` | `runWatchCycle()` — load state, collect commits, extract refs, append entries |
| Journal append | `internal/cli/watch_commands.go:535-548` | Opens file O_APPEND, JSON encodes per line |
| Journal read | `internal/engine/journal.go:12-81` | `ReadJournalEntries()`, `FilterJournalByTickets()`, `CountJournalForTicket()`, `LastNJournalEntries()` |
| Serve start | `internal/cli/watch_commands.go:175-252` | `runServeStart()` — spawns child process, writes PID file |
| Serve stop | `internal/cli/watch_commands.go:254-288` | `runServeStop()` — reads PID, sends SIGINT |
| Serve status | `internal/cli/watch_commands.go:290-322` | `runServeStatus()` — checks PID signal(0) |
| Serve logs | `internal/cli/watch_commands.go:324-374` | `runServeLogs()` — tails log file |
| Effort computation | `internal/journal/entry.go:42-58` | `Effort()` — sums LOC, deduplicates files |

**Adaptation notes:**
- tk uses bracket refs `[ticket-id]` in commit messages — same convention as tkt, no change needed
- tk's ticket IDs have different format (e.g., `migrate-tk-storage-eb6c`) vs tkt's `t-xxxx` — the regex extraction already handles arbitrary bracket content
- Consider integrating with tk's existing `auto_close` behavior if it has one
