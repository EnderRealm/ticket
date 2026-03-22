---
id: tic-7d1e
status: closed
deps: []
links: []
created: 2026-02-26T04:34:11Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, cli, mcp]
---
# Add tk next command



New CLI command: tk next [--project <epic-id>]. Shows per-project next actions and progress using Projects() from pkg/ticket/inbox.go. Both cmd/*.go and MCP tool (ticket_next). Also add ticket_dashboard MCP tool that combines inbox + projects + stats in one call.
