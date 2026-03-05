---
id: moveticket-drops-stage-05c8
stage: triage
status: open
deps: []
links: []
created: 2026-03-05T05:31:09Z
type: bug
priority: 2
tags: [move, pipeline]
---
# MoveTicket drops stage, review, risk fields and should reset stage to triage

MoveTicket in pkg/ticket/move.go manually copies individual fields when creating the destination ticket (lines 81-93). It misses Stage, Review, and Risk. This approach is also fragile — any new frontmatter field added to Ticket will be silently dropped unless move.go is updated.

Fix: Copy all frontmatter fields inclusively (shallow copy the struct), then override the fields that need to change: new ID, reset Stage to StageTriage, clear Review, add provenance note. This ensures future fields are preserved by default.

Found during verify of tk-ui-add-f586.
