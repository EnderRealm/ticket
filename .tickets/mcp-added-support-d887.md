---
id: mcp-added-support-d887
stage: done
status: closed
deps: []
links: []
created: 2026-03-07T02:46:39Z
type: feature
priority: 2
skipped: [spec, design, implement, test, verify]
---
# MCP: Added support for setting risk



## Notes

**2026-03-07T02:49:07Z**

<!-- checkpoint: executing -->

Added `risk` field to `createArgs` and `editArgs` structs in `internal/mcp/mcp.go`. Both handlers wire the field through to `ticket.RiskLevel`. Added `TestRiskField` integration test covering create-with-risk and edit-risk. All tests pass. CHANGELOG updated.
