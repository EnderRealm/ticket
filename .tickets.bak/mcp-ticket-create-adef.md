---
id: mcp-ticket-create-adef
stage: done
status: closed
deps: []
links: []
created: 2026-02-28T20:26:56Z
type: bug
priority: 0
skipped: [implement, test, verify]
---
# MCP: ticket_create doesn't set created datetime field

The CLI properly sets created datetime field on ticket create, but the MCP ticket_create tool doesn't

## Test Results

`go test ./...` passes. TestCreateTicket now asserts created field is non-zero. MCP handler sets `Created: time.Now().UTC()` matching CLI and TUI paths.

## Review Log

**2026-03-01T00:24:45Z [agent:ghost]**
APPROVED — One-line fix adding Created timestamp to MCP create handler. Test assertion added.
