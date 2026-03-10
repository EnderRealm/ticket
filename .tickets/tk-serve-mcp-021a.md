---
id: tk-serve-mcp-021a
stage: triage
status: open
deps: []
links: []
created: 2026-03-08T10:00:51Z
type: bug
priority: 2
assignee: Steve Macbeth
---
# tk serve MCP ticket_list returns string "null"

tk serve's MCP ticket_list returns literal string "null" instead of JSONL when no results. Workaround in callToolArray guards against this and returns [] instead of [null]. Not yet fixed in tk itself.

## Notes

**2026-03-10T05:58:18Z**

Moved from tk-serve-mcp-1dd9 in /Users/steve/code/forge
