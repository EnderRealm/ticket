---
id: change-description-front-fa8a
stage: triage
status: open
deps: []
links: []
created: 2026-03-10T02:17:47Z
type: feature
priority: 0
---
# Change description to a front matter field


Medium complexity, wide blast radius. Touches every layer.

Description currently lives as free text in the markdown body (everything before the first `## ` heading). Moving it to frontmatter means it becomes a YAML field like `assignee` or `priority`.

### What changes

| Layer | Files | What |
|-------|-------|------|
| Types | `ticket.go` | Add `Description string` field to struct |
| Parse/Serialize | `format.go` | Stop extracting description from body, add to frontmatter YAML output. `UpdateSection(body, "")` pattern goes away for description. `parseBody()` changes. |
| CLI | `create.go`, `edit.go` | Stop building body strings manually, set `t.Description` directly |
| MCP | `mcp.go` | `parseSections()` no longer extracts description from body. `toJSON()` reads `t.Description`. Create/edit handlers set field directly. |
| TUI | `form.go`, `tui.go` | `extractDescription()` goes away, read from `t.Description`. Create/edit handlers set field. |
| Gates | `gates.go` | Simplifies — check `t.Description != ""` instead of parsing `t.Body` |
| Tests | 5+ test files | Format, gates, store, workflow, form tests all reference body content |

### Tricky parts

The body still holds other structured sections (`## Design`, `## Acceptance Criteria`, `## Test Results`, `## Notes`, `## Review Log`). So `Body` doesn't go away — it just loses the description preamble. The `UpdateSection` / `parseSections` machinery still needs to exist for those other sections.

### Migration

Existing ticket files have description in the body. Need either a migration pass or backward-compatible parsing that falls back to body extraction when the frontmatter field is empty.

### Scope

~9 source files + ~5 test files. Not architecturally risky, but tedious and easy to miss an edge case in the body parsing changes.

**2026-03-10T05:15:41Z**

This is a test

**2026-03-10T05:16:27Z**

This is a test

**2026-03-10T05:30:56Z**

Test thist

**2026-03-10T05:31:27Z**

This is a test
this is a second test

**2026-03-10T05:31:37Z**

test
