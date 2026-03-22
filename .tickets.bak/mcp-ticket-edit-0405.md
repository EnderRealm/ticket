---
id: mcp-ticket-edit-0405
stage: done
status: open
deps: []
links: []
created: 2026-02-27T08:00:51Z
type: bug
priority: 1
assignee: Steve Macbeth
tags: [mcp]
---
# MCP ticket_edit silently drops body fields (acceptance, design, description)

When calling ticket_edit via MCP with acceptance, design, or description fields, the edit reports success but the values are not persisted to the ticket body. The CLI edit command writes these correctly. The MCP handler likely updates frontmatter only and doesn't rebuild the markdown body sections.

## Test Results

- [x] TestEditBodyFields: create ticket, edit with description/design/acceptance via MCP, verify all three persist on show
- [x] TestCreateTicket: regression check
- [x] TestAddNotePreservesNewlines: regression check
- [x] TestAddMultipleNotes: regression check
- [x] go test ./... passes (all packages)

## Review Log

**2026-02-27T08:11:35Z [agent:code-review]**
APPROVED — Moved updateSection to pkg/ticket as exported UpdateSection. MCP registerEdit now applies description/design/acceptance to t.Body. Test covers round-trip.

**2026-02-27T08:11:39Z [agent:impl-review]**
APPROVED — UpdateSection extracted to pkg/ticket, cmd/edit.go updated to use it, MCP handler applies all three body fields. No behavioral change to CLI path.

**2026-02-27T08:14:49Z [human:steve]**
APPROVED — Verified fix and test coverage.

## Notes

**2026-02-27T08:10:09Z**

## Triage\n\n**Risk:** low — isolated fix in MCP handler, mirrors existing CLI behavior\n\n**Scope:** single task\n\n**Root cause:** registerEdit handler reads description/design/acceptance from args struct but never applies them to t.Body. CLI edit.go uses updateSection() to merge these into the markdown body.\n\n**Fix:** Move updateSection to pkg/ticket, use it in both cmd/edit.go and internal/mcp/mcp.go.
