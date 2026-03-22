---
id: t-65bd
status: closed
deps: [t-3236, t-dcbb, t-afcf]
links: []
created: 2026-02-22T00:58:34Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# CLI commands: ls, ready, blocked, closed with filter flags

Implement listing commands with filter flags matching current bash output format.

## Design

Files: cmd/ls.go, cmd/ready.go, cmd/blocked.go, cmd/closed.go
Approach:
- ls: default shows open tickets. Shared filter flags: -a (assignee), -t (type), -T (tag), -P (priority), --status, --parent. list alias.
- ready: open/in_progress tickets with resolved deps and passing parent gate. --open flag bypasses parent gate.
- blocked: tickets with unresolved dependencies.
- closed: recently closed tickets sorted by mtime, --limit N flag (default 20).
- Output format: columnar table matching bash version exactly: ID, P(riority), TYPE, STATUS, TITLE
- --json flag outputs array of ticket objects instead of table.
- Shared filter flag registration via helper function.

## Acceptance Criteria

Output format matches bash version column-for-column. Filter flags work. Test suite ls/ready/blocked assertions pass.


## Notes

**2026-02-22T07:58:17Z**

Implemented ls (with list alias), ready (with --open flag), blocked, closed (with --limit, mtime sort). All share filter flags: -a, -t, -T, -P, --status, --parent. Output format matches bash version.
