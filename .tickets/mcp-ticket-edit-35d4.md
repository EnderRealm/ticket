---
id: mcp-ticket-edit-35d4
stage: backlog
deps: []
links: []
created: 2026-03-12T08:18:10Z
type: bug
priority: 0
tags: [mcp, gates]
---
# MCP ticket_edit appends duplicate body sections instead of replacing

When calling `ticket_edit` with `acceptance` or `design` parameters on a ticket that already has those sections, `UpdateSection` appends a new `## Acceptance Criteria` or `## Design` section instead of replacing the existing one. This creates duplicate sections in the ticket body. Subsequent reads may return stale content from the first section while the updated content sits in the duplicate.

**Reproduction:**
1. Create a ticket with acceptance criteria via `ticket_edit`
2. Call `ticket_edit` again with new acceptance criteria
3. Read the ticket file — two `## Acceptance Criteria` sections exist

**Root cause:** `UpdateSection` in `pkg/ticket/format.go` uses `"\n## "` to find section boundaries but appends a new section when it should be replacing the existing one.

**Impact:** Any iterative editing workflow via MCP (e.g., spec refinement, design iteration) corrupts the ticket body with duplicate sections. Discovered during design work on tk-ready-ls-554b.
