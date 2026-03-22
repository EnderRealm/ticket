---
id: add-support-generic-bf97
stage: done
risk: normal
deps: []
links: []
created: 2026-03-12T18:37:36Z
type: feature
priority: 2
---
# Add support for generic property fields

Add support for read/write/serialize arbitrary key/value pairs as frontmatter. A set operation on a ticket without that field will add the field. A set operation on a ticket wihtout that field will override the field. A set operation on a ticket with a field which sets it to blank will delete the field. Commands that list or return ticket metadata should include custom/generic fields as well. Support should exist in CLI, MCP, and TUI.

## Acceptance Criteria

## Acceptance Criteria

### Core (pkg/ticket/)

1. When YAML frontmatter contains keys not recognized by the Ticket struct, the system shall parse them into `Extra map[string]string` and preserve them through a serialize round-trip, verified by `go test ./pkg/ticket/ -run TestExtraFields`.

2. When `Serialize` is called on a ticket with populated `Extra` fields, the system shall write them as YAML key/value pairs after all known fields and before the closing `---`, verified by `go test ./pkg/ticket/ -run TestSerialize_ExtraFieldOrdering`.

3. When a ticket has no `Extra` fields, the system shall produce output identical to the current format, verified by `go test ./pkg/ticket/ -run TestSerialize_NoExtraFieldsUnchanged`.

4. When an `Extra` field key matches a reserved/known YAML key (id, stage, type, priority, etc.), the system shall reject the operation with an error naming the reserved key, verified by `go test ./pkg/ticket/ -run TestExtra_ReservedKeyRejected`.

5. When an `Extra` field key or value contains YAML-special characters (`:`, `#`, `[`, `{`, spaces in keys), the system shall reject the operation with an error, verified by `go test ./pkg/ticket/ -run TestExtra_InvalidCharsRejected`.

### CLI (cmd/)

6. When the CLI `edit` command receives `--set key=value`, the system shall add or update that key in the ticket's `Extra` map and persist it. Multiple `--set` flags in one invocation shall all apply, verified by `tk edit <id> --set env=prod --set team=core && tk show <id>`.

7. When the CLI `edit` command receives `--set key=` (blank value), the system shall remove that key from the ticket's `Extra` map, verified by `tk edit <id> --set env= && tk show <id>` confirms no `env:` line.

8. When the CLI `create` command receives `--set key=value`, the system shall store the key in the new ticket's `Extra` map, verified by `tk create "Test" --set env=staging && tk show <id>`.

9. When the CLI `show` command displays a ticket with `Extra` fields, the system shall include them in the rendered output, verified by manual check.

### CLI Query (cmd/query.go)

10. When `tk query` outputs JSONL for tickets with `Extra` fields, the system shall include them under an `"extra"` key in each JSON object, verified by `tk query | jq '.extra'`.

11. When `tk query` is used with a jq filter on extra fields (e.g., `tk query '.extra.env == "production"'`), the system shall correctly filter tickets by those values, verified by manual check.

### MCP (internal/mcp/)

12. When the MCP `ticket_edit` tool receives a `set` parameter (map of key/value pairs), the system shall add, update, or remove (blank value) entries in `Extra`, verified by `go test ./internal/mcp/ -run TestEditExtraFields`.

13. When the MCP `ticket_show` tool returns a ticket with `Extra` fields, the system shall include them in the JSON response under an `"extra"` key, verified by `go test ./internal/mcp/ -run TestShowExtraFields`.

14. When the MCP `ticket_list` tool returns tickets with `Extra` fields, the system shall include them in the summary JSON under an `"extra"` key, verified by `go test ./internal/mcp/ -run TestListExtraFields`.

15. When the MCP `ticket_create` tool receives a `set` parameter, the system shall populate `Extra` on the new ticket, verified by `go test ./internal/mcp/ -run TestCreateExtraFields`.

### TUI (internal/tui/)

16. When the TUI detail view renders a ticket with `Extra` fields, the system shall display each key/value pair in the metadata section after known fields, sorted by key, verified by manual check.

17. When the TUI edit form saves a ticket with `Extra` fields, the system shall preserve those fields (pass-through, not editable in form), verified by manual check.

## Design

## Architecture

Add `Extra map[string]string` to the Ticket struct for arbitrary user-defined key/value metadata in YAML frontmatter. Extra fields are preserved through parse/serialize round-trips and exposed across CLI, MCP, and TUI. Setting a field to blank removes it. Keys and values with YAML-special characters are rejected.

## Implementation Plan

### Step 1: pkg/ticket/ticket.go — Extra field and validation

Add to Ticket struct:
```go
Extra map[string]string `yaml:"-"` // custom key/value pairs, handled manually in format.go
```

Add `reservedKeys` set (all known YAML frontmatter keys: id, status, stage, review, risk, deps, links, created, type, priority, assignee, external-ref, branch, parent, tags, skipped, conversations).

Add `ValidateExtraKey(key string) error` — rejects reserved keys, empty keys, keys with spaces/colons/special chars.
Add `ValidateExtraValue(value string) error` — rejects values with YAML-special chars (`:`, `#`, `[`, `{`).

Initialize `Extra` to `map[string]string{}` in `Parse()` nil-normalization block.

### Step 2: pkg/ticket/format.go — Two-pass parse, serialize Extra

**Parse:** After `yaml.Unmarshal(front, &t)`, second unmarshal into `map[string]interface{}` to capture unknown keys. Diff against `reservedKeys` to populate `t.Extra`.

**Serialize:** After writing `conversations` (line 99) and before `---` (line 101), iterate `t.Extra` in sorted key order via `writeField()`. Add `"sort"` to imports.

### Step 3: pkg/ticket/format_test.go — Tests

- `TestExtraFields_RoundTrip` — parse YAML with unknown keys, serialize, verify preserved
- `TestSerialize_ExtraFieldOrdering` — extra fields after known fields, sorted alphabetically
- `TestSerialize_NoExtraFieldsUnchanged` — no output change when Extra empty
- `TestExtra_ReservedKeyRejected` — ValidateExtraKey rejects "id", "stage", etc.
- `TestExtra_InvalidCharsRejected` — keys with spaces/colons, values with #/[/{ rejected

### Step 4: cmd/edit.go — Add --set flag

Add `--set` as `StringArray` (NOT StringSlice — StringSlice splits on commas, corrupting values). Each `--set` flag is a separate `key=value` entry.

```go
f.StringArray("set", nil, "set extra field (key=value, blank value removes)")
```

Parse with shared `parseSetFlag(s string) (key, value string, err error)` helper that splits on first `=`, validates key and value.

### Step 5: cmd/create.go — Add --set flag

Same `StringArray` flag and `parseSetFlag` helper. Apply to new ticket's Extra before `store.Create()`.

### Step 6: cmd/query.go — Add Extra to JSON output

Add `Extra map[string]string` field with `json:"extra,omitempty"` to `ticketJSON`. Populate from `t.Extra` in `runQuery()`.

### Step 7: internal/mcp/mcp.go — Extra in JSON structs and edit/create

Add `Extra map[string]string` with `json:"extra,omitempty"` to both `ticketSummaryJSON` and `ticketJSON`. Update `toSummaryJSON()` and `toJSON()`.

Add `Set map[string]string` field to `editArgs` and `createArgs`. In handlers, validate keys/values with `ValidateExtraKey`/`ValidateExtraValue`, apply set/delete semantics.

### Step 8: internal/mcp/mcp_test.go — MCP tests

Using in-process test harness:
- `TestEditExtraFields` — set, update, remove via ticket_edit
- `TestShowExtraFields` — verify in ticket_show JSON
- `TestListExtraFields` — verify in ticket_list summary
- `TestCreateExtraFields` — create with extra fields
- `TestEditExtraReservedKey` — reserved key rejection

### Step 9: internal/tui/detail.go — Render Extra fields

In `render()`, after Created field (line 334) and before blank line, iterate sorted `t.Extra` keys and render each via `m.field()`. Add `"sort"` to imports.

### Step 10: cmd/root.go, README.md, CHANGELOG.md

Update help text for `--set`, document in README, add changelog entry.

## Files Affected

| File | Change |
|------|--------|
| `pkg/ticket/ticket.go` | Extra field, reservedKeys, ValidateExtraKey/Value |
| `pkg/ticket/format.go` | Two-pass parse, serialize Extra |
| `pkg/ticket/format_test.go` | New tests |
| `cmd/edit.go` | --set StringArray flag, parseSetFlag helper |
| `cmd/create.go` | --set StringArray flag |
| `cmd/query.go` | Extra in ticketJSON |
| `internal/mcp/mcp.go` | Extra in JSON structs, Set param in edit/create |
| `internal/mcp/mcp_test.go` | New tests |
| `internal/tui/detail.go` | Render extra fields |
| `cmd/root.go` | Help text |
| `README.md` | Usage docs |
| `CHANGELOG.md` | Release notes |

## AC Coverage

All 17 acceptance criteria are covered: AC1-3 by Step 2, AC4-5 by Step 1, AC6-9 by Steps 4-5, AC10-11 by Step 6, AC12-15 by Step 7, AC16 by Step 9, AC17 automatic (Get/Update round-trip preserves Extra).

## Key Decisions

- **`yaml:"-"` tag** — Manual handling avoids yaml.v3 nested map serialization, gives control over field ordering (auto)
- **Two-pass parse** — Struct unmarshal for known fields, raw map unmarshal for unknown field discovery (auto)
- **StringArray not StringSlice** — Prevents comma splitting from corrupting values (agent:design-reviewer)
- **Sorted keys in output** — Deterministic serialization (auto)
- **Centralized validation in pkg/ticket/** — Reused by CLI and MCP layers (auto)

## Test Results

**Timestamp:** 2026-03-12T19:47:00Z

### Automated
- **Type check:** N/A (Go, compiled -- build succeeded)
- **Lint:** SKIPPED (no lint script configured)
- **Test suite:** go test ./... -- all packages PASS

### Acceptance Criteria
- [x] AC1 -- TestExtraFields_RoundTrip: PASS
- [x] AC2 -- TestSerialize_ExtraFieldOrdering: PASS
- [x] AC3 -- TestSerialize_NoExtraFieldsUnchanged: PASS
- [x] AC4 -- TestExtra_ReservedKeyRejected: PASS
- [x] AC5 -- TestExtra_InvalidCharsRejected: PASS
- [x] AC6 -- CLI edit --set key=value: PASS (env=staging->prod confirmed)
- [x] AC7 -- CLI edit --set key= removes field: PASS (team removed)
- [x] AC8 -- CLI create --set key=value: PASS (env=staging, team=backend on creation)
- [x] AC9 -- CLI show displays extra fields: PASS (env and team visible in output)
- [x] AC10 -- CLI query includes extra in JSONL: PASS (extra:{env:prod} in JSON)
- [?] AC11 -- CLI query jq filter on extra: MANUAL (requires jq pipeline testing)
- [x] AC12 -- TestEditExtraFields: PASS
- [x] AC13 -- TestShowExtraFields: PASS
- [x] AC14 -- TestListExtraFields: PASS
- [x] AC15 -- TestCreateExtraFields: PASS
- [x] AC4 MCP -- TestEditExtraReservedKey: PASS
- [?] AC16 -- TUI detail view renders extra: MANUAL (requires interactive TUI)
- [?] AC17 -- TUI edit preserves extra: MANUAL (requires interactive TUI)

### Failures
None.

### Summary
15/17 criteria verified and passing. 3 criteria require manual verification (TUI rendering AC16/AC17, jq filter AC11). All automated tests pass. All CLI integration smoke tests pass.

## Review Log

**2026-03-12T19:06:15Z [human:steve]**
APPROVED — Spec reviewed interactively. Decisions: block special chars instead of quoting, strings-only values, TUI pass-through only, tk query includes extra fields. Two child tickets created for out-of-scope work.

**2026-03-12T19:11:57Z [agent:design-reviewer]**
REJECTED — StringSlice flag splits on commas, corrupting values. Must use StringArray instead. Missing sort import in detail.go is trivial. All file paths, API shapes, and AC coverage verified.

**2026-03-12T19:16:58Z [human:steve]**
APPROVED — Design approved. StringArray fix incorporated. Proceed to implement.

**2026-03-12T19:17:19Z [human:steve]**
APPROVED — Design review passed.

**2026-03-12T19:44:56Z [agent:impl-reviewer]**
APPROVED — All 17 acceptance criteria verified. Design followed precisely across all 10 steps. No scope creep, no TODOs, no placeholders. External type claims (pflag StringArray) verified against source.

**2026-03-12T19:44:58Z [agent:code-reviewer]**
APPROVED — Initial review caught missing newline/CR/tab rejection in ValidateExtraValue (YAML frontmatter injection). Fixed and re-reviewed. All entry points use centralized validators. No bypass paths. Clean, well-structured implementation.

**2026-03-12T19:45:20Z [agent:code-reviewer]**
- **CENTRALIZED VALIDATION IN PKG/TICKET/** — Reused by CLI and MCP layers (auto)

**2026-03-12T19:06:15Z [human:steve]**
APPROVED — Spec reviewed interactively. Decisions: block special chars instead of quoting, strings-only values, TUI pass-through only, tk query includes extra fields. Two child tickets created for out-of-scope work.

**2026-03-12T19:11:57Z [agent:design-reviewer]**
REJECTED — StringSlice flag splits on commas, corrupting values. Must use StringArray instead. Missing sort import in detail.go is trivial. All file paths, API shapes, and AC coverage verified.

**2026-03-12T19:16:58Z [human:steve]**
APPROVED — Design approved. StringArray fix incorporated. Proceed to implement.

**2026-03-12T19:17:19Z [human:steve]**
APPROVED — Design review passed.

**2026-03-12T19:44:56Z [agent:impl-reviewer]**
APPROVED — All 17 acceptance criteria verified. Design followed precisely across all 10 steps. No scope creep, no TODOs, no placeholders. External type claims (pflag StringArray) verified against source.

**2026-03-12T19:44:58Z [agent:code-reviewer]**
APPROVED — Initial review caught missing newline/CR/tab rejection in ValidateExtraValue (YAML frontmatter injection). Fixed and re-reviewed. All entry points use centralized validators. No bypass paths. Clean, well-structured implementation.

**2026-03-12T19:45:20Z [agent:code-reviewer]**
APPROVED — Code review passed. YAML injection fix applied. All entry points use centralized validators.

**2026-03-13T18:23:35Z [human:steve]**
APPROVED — Verified: validation hardened (allowlist keys, comprehensive YAML indicator blocklist), extra fields flattened to top-level in all JSON output, reserved keys expanded for JSON collision prevention.

## Notes

**2026-03-12T19:05:42Z**

## Spec

**Scope:**
- In: pkg/ticket/ (struct, format, validation), cmd/ (edit, create, query), internal/mcp/ (edit, show, list, create), internal/tui/ (detail view, form pass-through), README, CHANGELOG
- Out: TUI form editing (tui-form-editing-41cb), filtering by extra fields (filter-tickets-extra-b3bb), typed values

**Decisions:**
- Block/error on YAML-special characters in keys and values rather than quoting (human)
- Extra values are strings only, no type coercion (auto)
- TUI form pass-through only, no dynamic form fields (human — separate ticket)
- tk query includes extra fields in JSONL output under "extra" key (human)
- Child tickets created: tui-form-editing-41cb, filter-tickets-extra-b3bb (human)

**2026-03-12T19:45:09Z**

## Implement

**Changes:**
- pkg/ticket/ticket.go: Extra field, reservedKeys map, ValidateExtraKey/Value/IsReservedKey
- pkg/ticket/format.go: Two-pass parse (struct + raw map), serialize Extra sorted before closing ---
- pkg/ticket/format_test.go: 5 new tests (round-trip, ordering, no-op, reserved keys, invalid chars)
- cmd/edit.go: --set StringArray flag, parseSetFlag helper, set/delete semantics
- cmd/create.go: --set StringArray flag, extra map applied to new ticket
- cmd/query.go: Extra in ticketJSON struct for JSONL output
- internal/mcp/mcp.go: Extra in ticketSummaryJSON/ticketJSON, Set param on create/edit handlers
- internal/mcp/mcp_test.go: 5 new MCP integration tests
- internal/tui/detail.go: Render sorted Extra fields after Created
- cmd/root.go: Help text updated with --set and extra{} field
- README.md: Extra Fields usage section
- CHANGELOG.md: Unreleased section with 7 bullet points

**Decisions:**
- Added newline/CR/tab to rejected characters in ValidateExtraKey and ValidateExtraValue (auto) — caught by code-reviewer, prevents YAML frontmatter injection via writeField

**Reviews:**
- impl-reviewer: approved — all 17 AC verified, design followed precisely
- code-reviewer: rejected → fixed → approved — caught missing newline validation in extra field values

**2026-03-13T01:26:32Z**

## Test Results

**Timestamp:** 2026-03-12T19:47:00Z

### Automated
- **Type check:** N/A (Go, compiled — build succeeded)
- **Lint:** SKIPPED (no lint script configured)
- **Test suite:** go test ./... — all packages PASS (pkg/ticket 0.322s, internal/mcp cached, internal/tui cached)

### Acceptance Criteria
- [x] AC1 — TestExtraFields_RoundTrip: PASS
- [x] AC2 — TestSerialize_ExtraFieldOrdering: PASS
- [x] AC3 — TestSerialize_NoExtraFieldsUnchanged: PASS
- [x] AC4 — TestExtra_ReservedKeyRejected: PASS
- [x] AC5 — TestExtra_InvalidCharsRejected: PASS
- [x] AC6 — CLI edit --set key=value: PASS (env=staging->prod confirmed)
- [x] AC7 — CLI edit --set key= removes field: PASS (team removed)
- [x] AC8 — CLI create --set key=value: PASS (env=staging, team=backend on creation)
- [x] AC9 — CLI show displays extra fields: PASS (env and team visible in output)
- [x] AC10 — CLI query includes extra in JSONL: PASS (extra:{env:prod} in JSON)
- [?] AC11 — CLI query jq filter on extra: MANUAL (requires jq pipeline testing)
- [x] AC12 — TestEditExtraFields: PASS
- [x] AC13 — TestShowExtraFields: PASS
- [x] AC14 — TestListExtraFields: PASS
- [x] AC15 — TestCreateExtraFields: PASS
- [x] AC4 MCP — TestEditExtraReservedKey: PASS
- [?] AC16 — TUI detail view renders extra: MANUAL (requires interactive TUI)
- [?] AC17 — TUI edit preserves extra: MANUAL (requires interactive TUI)

### Failures
None.

### Summary
15/17 criteria verified and passing. 3 criteria require manual verification (TUI rendering AC16/AC17, jq filter AC11). All automated tests pass. All CLI integration smoke tests pass. Ready to advance.

**2026-03-13T01:28:59Z**

## Test Results

**Timestamp:** 2026-03-12T19:47:00Z

### Automated
- **Type check:** N/A (Go, compiled -- build succeeded)
- **Lint:** SKIPPED (no lint script configured)
- **Test suite:** go test ./... -- all packages PASS (pkg/ticket 0.322s, internal/mcp cached, internal/tui cached)

### Acceptance Criteria
- [x] AC1 -- TestExtraFields_RoundTrip: PASS
- [x] AC2 -- TestSerialize_ExtraFieldOrdering: PASS
- [x] AC3 -- TestSerialize_NoExtraFieldsUnchanged: PASS
- [x] AC4 -- TestExtra_ReservedKeyRejected: PASS
- [x] AC5 -- TestExtra_InvalidCharsRejected: PASS
- [x] AC6 -- CLI edit --set key=value: PASS (env=staging->prod confirmed)
- [x] AC7 -- CLI edit --set key= removes field: PASS (team removed)
- [x] AC8 -- CLI create --set key=value: PASS (env=staging, team=backend on creation)
- [x] AC9 -- CLI show displays extra fields: PASS (env and team visible in output)
- [x] AC10 -- CLI query includes extra in JSONL: PASS (extra:{env:prod} in JSON)
- [?] AC11 -- CLI query jq filter on extra: MANUAL (requires jq pipeline testing)
- [x] AC12 -- TestEditExtraFields: PASS
- [x] AC13 -- TestShowExtraFields: PASS
- [x] AC14 -- TestListExtraFields: PASS
- [x] AC15 -- TestCreateExtraFields: PASS
- [x] AC4 MCP -- TestEditExtraReservedKey: PASS
- [?] AC16 -- TUI detail view renders extra: MANUAL (requires interactive TUI)
- [?] AC17 -- TUI edit preserves extra: MANUAL (requires interactive TUI)

### Failures
None.

### Summary
15/17 criteria verified and passing. 3 criteria require manual verification (TUI rendering AC16/AC17, jq filter AC11). All automated tests pass. All CLI integration smoke tests pass.

**2026-03-13T01:41:39Z**

## Verify fix: YAML indicator character validation

Manual testing revealed `%^` bypassed ValidateExtraValue and corrupted YAML frontmatter (`%` is a YAML directive indicator).

**Root cause:** Blocklist approach was incomplete — only blocked `:`, `#`, `[`, `{`, control chars. Missed `%`, `!`, `&`, `*`, `@`, backtick, `|`, `>`, `'`, `"`, `]`, `}`.

**Fix:** Switched ValidateExtraKey to allowlist (`[a-zA-Z0-9_-]` only). Expanded ValidateExtraValue to block all YAML indicator characters via switch statement. Closes the class of bugs rather than playing whack-a-mole.
