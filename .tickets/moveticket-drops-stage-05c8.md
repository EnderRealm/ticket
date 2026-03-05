---
id: moveticket-drops-stage-05c8
stage: verify
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

## Test Results

All tests pass: go test ./... — pkg/ticket (including new move_test.go), internal/mcp, internal/tui all green.

## Review Log

**2026-03-05T05:36:00Z [agent:impl-reviewer]**
APPROVED — Shallow copy approach is correct. All slice fields properly deep-copied to prevent aliasing. Stage reset to triage, Review cleared. Tests cover field preservation and slice isolation.

**2026-03-05T05:38:12Z [agent:code-reviewer]**
APPROVED — Shallow copy correct. Status reset to open, Skipped cleared, slices deep-copied. Review findings addressed.
