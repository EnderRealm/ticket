---
id: tic-33fc
status: closed
deps: []
links: []
created: 2026-02-26T04:34:01Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, cli, mcp]
---
# Add tk review command



New CLI command: tk review <id> --approve|--reject [--comment '...'] [--actor '...']. Records review verdict on ticket. Appends to Review Log section. Updates review YAML field. Both cmd/*.go and MCP tool (ticket_review).
