---
id: tui-structured-filter-030c
stage: backlog
risk: normal
deps: []
links: []
created: 2026-03-22T18:19:15Z
type: task
priority: 2
parent: tui-revamp-port-0365
tags: [tui, tkt-port]
---
# TUI structured filter system with contextual picker

Replace ticket's basic type-cycle + text-search filtering with tkt's structured multi-dimensional filter system. This is a significant UX upgrade — currently there's no way to filter by assignee, tag, priority, or parent in the TUI.

**What to build:**

1. **Filter input bar** — activated with `/`, supports prefix syntax:
   ```
   type:bug priority:0 assignee:alice tag:backend stage:implement some search text
   ```
   - Tokens with `:` parsed as key:value
   - Remaining text is free-text search (case-insensitive substring on ID + title + description)
   - AND semantics (all criteria must match)

2. **Filter dimensions:**
   - `type:` — exact match (bug, feature, task, epic, chore)
   - `priority:` — exact numeric match (0-4)
   - `assignee:` — case-insensitive exact match
   - `tag:` — case-insensitive exact match
   - `stage:` — exact match on pipeline stage
   - `parent:` — exact match on parent ID (filter to epic children)
   - `risk:` — exact match (low, normal, high, critical)
   - Free text — substring search on ID, title, description

3. **Contextual filter picker** — `f` key on any ticket builds filter options from that ticket's fields:
   - If ticket has parent → "Children of [parent-id]" option
   - If ticket IS an epic → "Children of [this-id]" option
   - Assignee, tags, type, priority, stage, risk as one-touch options
   - Navigate with j/k, Enter selects, Esc cancels

4. **Filter persistence** — active filter shown in status bar across all views (dashboard, pipeline, detail, epic)

5. **Clear** — Esc on board/pipeline clears active filter

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Filter struct | `internal/tui/filter.go:12-20` | `filter` with text, ticketType, assignee, tag, priority, status, parent |
| Parse | `internal/tui/filter.go:22-60` | `parseFilter()` — tokenize and assign dimensions |
| Apply | `internal/tui/filter.go:62-95` | `applyFilter()` — AND across all dimensions |
| Contextual picker | `internal/tui/filter.go:97-140` | `filterOptionsForTicket()` — builds options from ticket fields |
| Input mode | `internal/tui/app.go` | filterInput state, text input handling |

**Adaptation notes:**
- Add `stage:` and `risk:` dimensions (ticket-specific, not in tkt)
- ticket's existing tab filtering (Backlog, Triage, Inbox, Done, All) should compose with structured filters — tabs set the base set, structured filter narrows within it
- Keep the `t` key as a quick shortcut for type cycling (common action)
