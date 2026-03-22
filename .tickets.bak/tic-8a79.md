---
id: tic-8a79
status: closed
deps: []
links: []
created: 2026-02-26T04:34:28Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, workspace]
---
# Implement workspace mode for multi-repo ticket aggregation




New file pkg/ticket/workspace.go. Workspace struct holding multiple FileStores. Qualified IDs (repo:ticket-id). Workspace.List(), Get(), Inbox(), Projects() aggregate across repos. Config via ~/.config/tk/workspace.yaml or TK_WORKSPACE env var. CLI: tk inbox -w / --workspace flag. MCP: tk serve --workspace. Cross-repo dependencies resolve across repo boundaries. StoreInterface interface implemented by both FileStore and Workspace.

## Notes

**2026-02-26T05:20:40Z**

Deferred — workspace mode is orthogonal to pipeline work. Will be a separate ticket.
