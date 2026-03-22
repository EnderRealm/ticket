---
id: t-cce1
status: closed
deps: [t-ad86]
links: []
created: 2026-02-22T00:59:22Z
type: task
priority: 2
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-3]
---
# MCP server: core tools

Implement tk serve as a stdio MCP server with ticket management tools.

## Design

Files: cmd/serve.go, internal/mcp/server.go, internal/mcp/tools.go
Approach:
- cmd/serve.go: tk serve command, starts MCP server on stdio (stdin/stdout JSON-RPC)
- server.go: MCP protocol handling - initialize, tools/list, tools/call dispatch
- tools.go: tool definitions and handlers:
  - ticket_list: params (status, type, priority, assignee, tag) → JSON array of tickets
  - ticket_show: params (id) → full ticket JSON
  - ticket_create: params (title, description, design, acceptance, type, priority, assignee, parent, tags, external_ref) → created ticket JSON
  - ticket_edit: params (id, + any editable field) → updated ticket JSON
  - ticket_add_note: params (id, text) → updated ticket JSON
  - ticket_dep: params (id, dep_id, action: add|remove) → updated ticket JSON
  - ticket_link: params (id, target_id, action: add|remove) → updated ticket JSON
  - ticket_ready: params (same filters as list) → JSON array
  - ticket_blocked: params (same filters) → JSON array
  - ticket_workflow: no params → workflow guide text
- All handlers use pkg/ticket core, return structured JSON
- Use mcp-go SDK or implement minimal stdio protocol directly
- Claude Code config: {"mcpServers": {"tk": {"command": "tk", "args": ["serve"]}}}

## Acceptance Criteria

tk serve responds to MCP initialize and tools/list. All tools return correct JSON. Claude Code can connect and use tools.


## Notes

**2026-02-22T19:47:46Z**

MCP server implemented with 10 tools: ticket_list, ticket_show, ticket_create, ticket_edit, ticket_add_note, ticket_dep, ticket_link, ticket_ready, ticket_blocked, ticket_workflow. Uses official modelcontextprotocol/go-sdk. All tools tested via stdio JSON-RPC.
