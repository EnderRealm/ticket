---
id: verify-writers-against-a372
stage: backlog
deps: [update-forge-dashboard-6a21, update-orchestrator-drop-7d90]
links: []
created: 2026-03-22T04:49:20Z
type: feature
priority: 2
parent: eb6c
tags: [tk, storage]
---
# Verify all writers against central ticket store

End-to-end verification that all three ticket writers work correctly against the central store.

**Verify:**
1. Forge — interactive UX ticket changes via MCP
2. Forge Pipeline — ticket stage transitions via MCP
3. tk ui / CLI — human-initiated ticket edits

**Test scenarios:**
- Create, edit, advance, and close tickets from each writer
- Confirm no stale reads or write conflicts under normal usage
