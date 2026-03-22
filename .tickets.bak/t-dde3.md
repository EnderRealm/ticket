---
id: t-dde3
status: closed
deps: []
links: []
created: 2026-02-22T00:57:39Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# Core types and validation



Define Ticket struct, Status/TicketType enums, Note type, and validation functions.

## Design

Files: pkg/ticket/ticket.go
Approach:
- Ticket struct with yaml tags: id, status, type, priority, assignee, parent, deps, links, tags, external-ref, created
- Title parsed from markdown heading (yaml:"-")
- Body sections: Description, Design, Acceptance, Notes
- Status enum: open, in_progress, needs_testing, closed
- TicketType enum: task, feature, bug, epic, chore
- Note struct: Timestamp + Text
- ValidateStatus(), ValidateType(), ValidatePriority(0-4) functions
- flow tag on deps/links/tags for inline YAML arrays

## Acceptance Criteria

Types compile, validation rejects invalid status/type/priority

## Notes

**2026-02-22T01:53:27Z**

Implemented: Ticket struct with yaml tags, Status/TicketType enums, Note type, ValidateStatus/ValidateType/ValidatePriority functions, Ticket.Validate(). Tests cover all validation paths.
