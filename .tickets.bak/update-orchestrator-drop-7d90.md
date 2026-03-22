---
id: update-orchestrator-drop-7d90
stage: backlog
deps: [create-central-tickets-3a39]
links: []
created: 2026-03-22T04:49:14Z
type: feature
priority: 2
parent: eb6c
tags: [architecture, tk, storage, forge]
---
# Update orchestrator to drop worktree ticket file sync

Remove worktree ticket file sync from the orchestrator. With tickets in a central repo, there's no need to copy ticket files between worktrees or branches.

**Scope:**
- Remove ticket file copy logic in worktree setup
- Remove any stash/pull/push cycles related to ticket files
- Verify orchestrator pipeline still reads/writes tickets correctly via MCP
