---
id: clean-project-tickets-750b
stage: backlog
deps: [update-forge-dashboard-6a21, update-orchestrator-drop-7d90]
links: []
created: 2026-03-22T04:49:17Z
type: feature
priority: 3
parent: eb6c
tags: [tk, storage]
---
# Clean up in-project .tickets dirs and update .gitignore

After all consumers are migrated to the central store, remove .tickets/ directories from project repos and update .gitignore files.

**Scope:**
- Remove .tickets/ from each project repo
- Add .tickets/ to .gitignore in project repos (prevent accidental re-creation)
- Commit cleanup in each project
