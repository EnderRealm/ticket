---
id: context-command-complete-f9f3
stage: backlog
risk: normal
deps: [commit-journal-background-e2be]
links: []
created: 2026-03-22T04:00:38Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [views, tkt-port]
---
# Context command — complete working context for a ticket

Add `tk context <id>` command that gives everything you need to understand a ticket in one call. Critical for agents — one MCP tool call to get full situational awareness.

**What it shows:**
- Full ticket data (all fields, body, acceptance criteria)
- Parent ticket (if any) with status
- Dependencies with status (including "missing" for dangling refs)
- Dependents (reverse deps — who depends on this ticket)
- Linked tickets with status
- Children (if this is an epic)
- Recent commits (last 10 linked to this ticket)

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Context command | `internal/cli/context_command.go:12-177` | `runContext()` — loads all tickets + journal, assembles composite view |
| Parent resolution | Same file, lines 31-35 | Lookup `record.Front.Parent` in byID map |
| Forward deps | Same file, lines 38-53 | Resolve each dep ID, mark "missing" if not found |
| Reverse deps | Same file, lines 56-68 | Scan all records for deps containing this ID |
| Links | Same file, lines 71-86 | Resolve each link ID, handle missing |
| Children | Same file, lines 89-95 | Filter records where Parent == this ID |
| Recent commits | Same file, lines 98-104 | `FilterJournalByTickets` + `LastNJournalEntries(filtered, 10)` |

**Adaptation notes:**
- tk has richer data per ticket (stage, risk, reviews, skipped stages) — include all of these in context output
- tk's `show` command already shows some of this — context is the superset for agents
- Consider including review history and pipeline stage in the context output
- This becomes the primary MCP tool agents use to understand what they're working on
