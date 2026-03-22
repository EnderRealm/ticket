---
id: add-branch-frontmatter-b7df
stage: done
status: closed
deps: []
links: []
created: 2026-03-05T03:43:51Z
type: feature
priority: 0
---
# Add branch to frontmatter




Add a `branch` string field to ticket YAML frontmatter to track which git branch a ticket's work is on. Includes full stack: struct field, serialization, MCP exposure, CLI edit flag, TUI form field.

## Acceptance Criteria

1. When a ticket has a `branch` field set, `tk show <id>` and MCP `ticket_show` shall include the branch value in output, verified by creating a ticket with branch set and confirming it appears.
2. When `tk edit <id> --branch <name>` is run, the branch field shall be persisted to YAML frontmatter and survive round-trip serialization, verified by editing then showing the ticket.
3. When MCP `ticket_edit` is called with a `branch` parameter, the branch field shall be persisted identically to CLI edit, verified via in-process MCP test.
4. When MCP `ticket_create` is called with a `branch` parameter, the branch field shall be set on the new ticket, verified via in-process MCP test.
5. When a ticket has no branch set, the field shall be omitted from frontmatter (omitempty), verified by confirming no `branch:` line appears in a ticket without one set.
6. When editing a ticket in `tk ui`, the branch field shall be visible and editable in the edit form.

## Design

Add `Branch` string field to ticket frontmatter. 6 files touched, all following existing patterns.

### 1. pkg/ticket/ticket.go
- Add `Branch string \`yaml:"branch,omitempty"\`` to Ticket struct, after `ExternalRef`

### 2. pkg/ticket/format.go — Serialize()
- After the `external-ref` conditional block (line 79), add:
```go
if t.Branch != "" {
    writeField(&buf, "branch", t.Branch)
}
```
- Parse is automatic via yaml.Unmarshal — no changes needed

### 3. internal/mcp/mcp.go
- Add `Branch string \`json:"branch,omitempty"\`` to `ticketJSON` struct
- Add `Branch string \`json:"branch,omitempty" jsonschema:"git branch name"\`` to `createArgs` struct
- Add `Branch string \`json:"branch,omitempty" jsonschema:"git branch name"\`` to `editArgs` struct
- In `toJSON()`: add `Branch: t.Branch`
- In `registerCreate()`: add `if args.Branch != "" { t.Branch = args.Branch }`
- In `registerEdit()`: add `if args.Branch != "" { t.Branch = args.Branch }`

### 4. cmd/edit.go
- Add flag: `f.String("branch", "", "git branch name")`
- Add handler block following `external-ref` pattern:
```go
if v, _ := cmd.Flags().GetString("branch"); cmd.Flags().Changed("branch") {
    t.Branch = v
    changed = true
}
```

### 5. internal/tui/form.go
- No change needed for initial implementation. Branch is a metadata field set by CLI/MCP, not typically edited in TUI. If we add it to TUI later, it follows the same text field pattern as Assignee.

**Decision:** Skip TUI form field for now (auto). Branch is set programmatically by forge workflow, not manually in TUI. AC6 can be deferred or dropped.

### 6. Tests — internal/mcp/mcp_test.go
- Add test: create ticket with branch, verify it appears in show output
- Add test: edit ticket with branch, verify persistence

## Test Results

- [x] TestCreateTicketWithBranch: create with branch, verify in response
- [x] TestEditTicketBranch: create without branch, edit to add, verify round-trip
- [x] All existing tests pass: go test ./... green
- [x] go build ./... compiles clean

## Review Log

**2026-03-05T04:01:07Z [human:steve]**
APPROVED — Acceptance criteria approved

**2026-03-05T04:24:11Z [human:steve]**
APPROVED — Design approved. No --branch on create, include README/help/changelog updates.

**2026-03-05T04:28:17Z [agent:code-reviewer]**
APPROVED — Approved. Clean additive change following ExternalRef pattern. Test hygiene fixes applied for swallowed errors.

**2026-03-05T04:28:19Z [agent:impl-reviewer]**
APPROVED — All 5 runtime AC satisfied with test coverage. AC6 (TUI) correctly deferred. No scope creep.

**2026-03-05T04:32:30Z [human:steve]**
APPROVED — CLI smoke test passed. All ACs verified.

## Notes

**2026-03-05T03:49:38Z**

## Triage

**Risk:** low — additive string field, follows existing patterns exactly. No behavioral changes to existing functionality.

**Scope:** single ticket. Merged add-ability-update-fa4c (duplicate).

**Touches:** pkg/ticket/ticket.go, pkg/ticket/format.go, internal/mcp/mcp.go, cmd/edit.go, internal/tui/form.go

**Decision:** Merge add-ability-update-fa4c into this ticket (human)

**2026-03-05T04:24:09Z**

Design review feedback:
- Skip --branch on cmd/create.go — branch is set post-creation when work begins (human)
- Add README.md and cmd/root.go help text updates (auto)
- Add CHANGELOG.md entry (auto)
- AC6 (TUI) deferred (auto)
