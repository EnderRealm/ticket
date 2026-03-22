---
id: update-forge-dashboard-6a21
stage: backlog
deps: [create-central-tickets-3a39]
links: []
created: 2026-03-22T04:49:11Z
type: feature
priority: 2
parent: eb6c
tags: [architecture, tk, storage, forge]
---
# Update Forge dashboard to use central ticket store

Remove the service-dir clone logic from the Forge dashboard. Instead of cloning each project repo to read its tickets, read directly from the central ticket store.

**Scope:**
- Drop ensureServiceDir / service-dir sync for ticket access
- Remove stale-clone refresh logic
- Point dashboard ticket reads at TICKETS_DIR
