---
id: tic-7dfd
status: closed
deps: []
links: []
created: 2026-02-26T04:34:23Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, cli, compat]
---
# Add backward compatibility for status-based commands




tk status <id> <status> still works but prints deprecation warning and maps to tk advance. ticket_edit with status field maps to stage internally. tk ls --status open maps to appropriate stage filter. One release of dual support before hard cut.
