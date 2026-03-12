---
id: mcp-error-ticket-b844
stage: done
deps: []
links: []
created: 2026-03-11T01:06:21Z
type: bug
priority: 2
---
# MCP: error on ticket_list tool for large ticket repos

`ticket_list` MCP tool fails when the result exceeds Claude Code's maximum allowed token limit. A repo with enough tickets produces a JSON response of ~185K characters, which gets rejected by the MCP client. The output is dumped to a temp file instead of being returned inline, breaking the tool contract.

**Root cause:** No server-side pagination or result-size cap on `ticket_list`. The tool returns all matching tickets in a single JSON response regardless of count.

**Impact:** Any agent calling `ticket_list` on a large repo gets an error instead of results. The workaround (reading chunks from a temp file) defeats the purpose of the MCP tool.

**Error message:**
```
Error: result (185,040 characters) exceeds maximum allowed tokens.
Output has been saved to [temp file path].
```

## Test Results

All 101 tests pass across 3 packages (internal/mcp, internal/tui, pkg/ticket).\n\nPagination tests:\n- TestListEmptyReturnsArray: empty result returns {tickets: [], total: 0}\n- TestListPagination: limit=2, offset=3, limit=0 (unlimited)\n- TestListDefaultLimitUnderCap: 3 tickets returns all, limit=50\n- TestListDefaultLimitCapsResults: 55 tickets capped at 50, total=55\n\nSummary fields test:\n- TestListReturnsSummaryFields: verifies body fields (description, design, acceptance_criteria, test_results, notes, reviews) are absent from list response\n\nNo regressions.

## Review Log

**2026-03-12T05:31:39Z [agent:code-reviewer]**
APPROVED — Approved. Clean pagination implementation, correct edge case handling. Added TestListDefaultLimitCapsResults per suggestion to exercise the >50 cap path.

**2026-03-12T05:31:40Z [agent:impl-reviewer]**
APPROVED — All acceptance criteria met. Pagination applied post-filter/post-sort, response envelope includes total/offset/limit, tests cover empty, pagination, default cap, and >50 cap scenarios. Breaking response shape change documented in CHANGELOG.

**2026-03-12T05:36:56Z [agent:code-reviewer]**
APPROVED — Approved. Added ticketSummaryJSON with metadata-only fields for list responses. Full ticketJSON retained for ticket_show. Summary test verifies body fields are excluded.

**2026-03-12T05:36:59Z [agent:impl-reviewer]**
APPROVED — Summary-only list response satisfies the original bug (185K response) more effectively than pagination alone. Combined with default limit of 50, response sizes are bounded by both field count and ticket count.

**2026-03-12T05:40:43Z [human:steve]**
APPROVED — Verified. Pagination + summary fields approach approved.

## Notes

**2026-03-11T01:07:38Z**

Observed in forge project (~185K chars). Claude Code MCP client rejected the response and saved it to disk. The tool needs `offset`/`limit` pagination params or a default result cap to stay within MCP response size limits.

**2026-03-12T05:35:33Z**

**Decision:** (human) Return summary fields only in ticket_list responses. Full detail stays in ticket_show. Summary: id, title, stage, review, risk, type, priority, assignee, parent, tags, deps, links, created. Drops: description, design, acceptance_criteria, test_results, notes, reviews.

**2026-03-12T05:35:46Z**

Reverted from verify to implement: Adding summary-only fields to ticket_list per human decision during verify
