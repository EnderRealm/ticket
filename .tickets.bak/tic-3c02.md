---
id: tic-3c02
status: closed
deps: []
links: []
created: 2026-02-26T04:34:07Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, cli, mcp]
---
# Add tk migrate command



New CLI command: tk migrate [--dry-run]. Rewrites all tickets from status to stage field. Dry-run shows changes without writing. Uses migration logic from pkg/ticket/migrate.go. Both cmd/*.go and MCP tool (ticket_migrate).
