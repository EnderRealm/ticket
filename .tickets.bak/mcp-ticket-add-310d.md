---
id: mcp-ticket-add-310d
stage: done
status: open
deps: []
links: []
created: 2026-02-27T07:42:31Z
type: bug
priority: 2
assignee: Steve Macbeth
tags: [mcp]
---
# MCP ticket_add_note splits text on double newline into multiple notes











When adding notes via the MCP ticket_add_note tool, notes appear multiple times in the ticket file with garbled formatting — fields concatenated without newlines, missing line breaks between sections. See show-encouraging-message-f072 Notes section for example: 4 duplicate triage entries and 3 duplicate spec entries with mangled text.

## Test Results

- [x] TestAddNotePreservesNewlines: single note with `**bold**` markdown round-trips correctly
- [x] TestAddMultipleNotes: two notes with `**bold**` lines and blank lines preserved as exactly 2 notes
- [x] TestCreateTicket: ticket creation still works (regression check)
- [x] go test ./... passes (all packages)

## Review Log

**2026-02-27T08:06:06Z [agent:code-review]**
APPROVED — Single-line reorder fix in parseNotes. Validates before flushing. Test reproduces and confirms.

**2026-02-27T08:08:12Z [agent:impl-review]**
APPROVED — Fix reorders parseNotes logic: validate timestamp before flushing. Test covers multi-note with bold markdown. Root cause addressed.

**2026-02-27T08:09:07Z [human:steve]**
APPROVED — Verified fix and test coverage.

## Notes

**2026-02-27T08:06:02Z**

Root cause: parseNotes flushed the current note before validating the timestamp. Lines matching **...** pattern that failed timestamp parsing were appended to a zombie note that had already been flushed to the slice. Fix: validate timestamp first, only flush on successful parse.
