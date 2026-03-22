---
id: tk-serve-mcp-021a
stage: done
status: in_progress
deps: []
links: []
created: 2026-03-08T10:00:51Z
type: bug
priority: 2
assignee: Steve Macbeth
skipped: [implement, test, verify]
---
# tk serve MCP ticket_list returns string "null"

tk serve's MCP ticket_list returns literal string "null" instead of JSONL when no results. Workaround in callToolArray guards against this and returns [] instead of [null]. Not yet fixed in tk itself.

## Notes

**2026-03-10T05:58:18Z**

Moved from tk-serve-mcp-1dd9 in /Users/steve/code/forge

**2026-03-12T04:22:39Z**

Root cause: var result []ticketJSON is nil when no tickets match. json.Marshal(nil) produces "null". Three occurrences: ticket_list (line 254), ticket_ready (line 603), ticket_blocked (line 634). Fix: initialize as empty slice literal.
