---
id: schema-preservation-round-6a37
stage: backlog
risk: normal
deps: []
links: []
created: 2026-03-22T04:01:07Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [architecture, tkt-port]
---
# Schema preservation — round-trip unknown YAML fields

Add an `Extra` map to the frontmatter model that preserves unknown YAML fields during parse/marshal cycles. Currently tk's strict struct drops any unrecognized keys — this means external tools or future extensions can't add custom metadata without tk silently destroying it.

**What to build:**
- `ExtraField` type with `Raw string` and `Block bool` (distinguishes single-line values from multi-line indented blocks)
- `Extra map[string]ExtraField` field on the frontmatter struct
- Capture unknown keys during YAML parsing into Extra map
- Emit Extra fields (sorted alphabetically) during YAML marshaling
- Preserve block indentation and newlines exactly as captured

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| ExtraField type | `internal/ticket/model.go:28-30` | `ExtraField{Raw string, Block bool}` |
| Extra on Frontmatter | `internal/ticket/model.go:17` | `Extra map[string]ExtraField` |
| Parse unknown keys | `internal/ticket/frontmatter.go:134-185` | `assignFrontmatterField()` default case stores in Extra |
| Block detection | `internal/ticket/frontmatter.go:105-123` | Multi-line blocks: key with no value, indented continuation lines |
| Marshal Extra | `internal/ticket/frontmatter.go:222-244` | Sorted keys, block or inline output |
| Round-trip test | `internal/ticket/frontmatter_test.go:10-50` | `TestRoundTripPreservesKnownAndUnknownFrontmatter()` |
| Test fixture | `testdata/tickets/task_with_custom.md` | `custom_field: keep-me` + `custom_map:\n  nested: true` |

**Adaptation notes:**
- tk uses `gopkg.in/yaml.v3` for parsing — may need to intercept at the map level before struct assignment, or switch to a two-pass parse (first to map, then extract known keys, remainder to Extra)
- The `set` parameter on tk's MCP `ticket_edit` tool already supports arbitrary key-value pairs — Extra gives those a proper home in the data model
- Extra fields should NOT appear in JSON output (they're YAML-only preservation) — matches tkt's behavior
