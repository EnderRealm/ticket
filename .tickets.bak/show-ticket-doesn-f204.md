---
id: show-ticket-doesn-f204
stage: done
deps: []
links: []
created: 2026-03-15T01:50:40Z
type: bug
priority: 0
skipped: [triage, implement, test, verify]
---
# Show ticket doesn't include branch

Show ticket doesn't include branch or other custom fields

## Notes

**2026-03-15T05:46:56Z**

Cannot duplicate. Both MCP ticket_show and CLI tk show include branch and extra fields. MCP: toJSON maps Branch (mcp.go:177) and Extra (mcp.go:188) with custom MarshalJSON. CLI: Serialize includes branch (format.go:97) and extras (format.go:116). Verified with live test.
