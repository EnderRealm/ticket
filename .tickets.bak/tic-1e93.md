---
id: tic-1e93
status: closed
deps: []
links: []
created: 2026-02-26T04:34:02Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, cli, mcp]
---
# Add tk skip command



New CLI command: tk skip <id> --to <stage> --reason '...'. Alias for advance --to with mandatory reason. Records skipped stages in ticket's skipped field. Both cmd/*.go and MCP tool (ticket_skip).
