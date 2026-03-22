---
id: gate-checks-require-0621
stage: done
status: open
deps: []
links: []
created: 2026-02-27T07:42:26Z
type: bug
priority: 1
assignee: Steve Macbeth
tags: [mcp, gates]
---
# Gate checks require body sections but ticket_edit writes to frontmatter










Gates at spec->design, design->implement, and test->verify check for markdown body sections (## Acceptance Criteria, ## Design, ## Test Results) but ticket_edit writes acceptance and design fields to YAML frontmatter only. This forces manual editing of .tickets/ files to pass gates, breaking the MCP-only workflow. Either gates should check frontmatter fields, or ticket_edit should write body sections, or both.

## Test Results

- [x] TestEditBodyFields: description, design, acceptance, and test_results round-trip through MCP edit+show
- [x] test_results field doesn't clobber existing description/design fields
- [x] go test ./... passes (all packages)

## Review Log

**2026-02-27T08:17:40Z [agent:code-review]**
APPROVED — Added test_results to editArgs, parseSections, toJSON. Completes MCP-only workflow for all gate-checked body sections. Test extended to cover round-trip.

**2026-02-27T08:17:44Z [agent:impl-review]**
APPROVED — test_results added to editArgs struct, handler applies via UpdateSection, parseSections extracts it, toJSON exposes it. All gate-checked sections now MCP-writable.

**2026-02-27T08:18:58Z [human:steve]**
APPROVED — Verified. All gate-checked body sections writable via MCP.

## Notes

**2026-02-27T08:17:32Z**

Root cause: MCP ticket_edit didn't write body fields (fixed in mcp-0405). Remaining gap was ## Test Results — no MCP field existed. Fix: added test_results to editArgs and parseSections/toJSON. All gate-checked body sections are now writable via MCP ticket_edit.
