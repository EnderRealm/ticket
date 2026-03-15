---
id: ticket-create-default-ff5f
stage: done
deps: []
links: []
created: 2026-03-15T02:06:34Z
type: bug
priority: 0
skipped: [triage, implement, test, verify]
---
# ticket_create default ticket stage should be backlog



## Notes

**2026-03-15T05:53:33Z**

## Triage

**Resolution:** Cannot reproduce. Both MCP `ticket_create` (mcp.go:446) and CLI `tk create` (create.go:115) already default to `StageBacklog`. No `stage` parameter is exposed in `createArgs`, so callers cannot override it.

**Decision:** Closing as cannot duplicate. (human)
