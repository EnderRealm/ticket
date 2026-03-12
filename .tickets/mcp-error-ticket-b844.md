---
id: mcp-error-ticket-b844
stage: triage
status: open
deps: []
links: []
created: 2026-03-11T01:06:21Z
type: bug
priority: 2
---
# MCP: error on ticket_list tool for large ticket repos

`ticket_list` MCP tool fails when the result exceeds Claude Code's maximum allowed token limit. A repo with enough tickets produces a JSON response of ~185K characters, which gets rejected by the MCP client. The output is dumped to a temp file instead of being returned inline, breaking the tool contract.

**Root cause:** No server-side pagination or result-size cap on `ticket_list`. The tool returns all matching tickets in a single JSON response regardless of count.

**Impact:** Any agent calling `ticket_list` on a large repo gets an error instead of results. The workaround (reading chunks from a temp file) defeats the purpose of the MCP tool.

**Error message:**
```
Error: result (185,040 characters) exceeds maximum allowed tokens.
Output has been saved to [temp file path].
```

## Notes

**2026-03-11T01:07:38Z**

Observed in forge project (~185K chars). Claude Code MCP client rejected the response and saved it to disk. The tool needs `offset`/`limit` pagination params or a default result cap to stay within MCP response size limits.
