---
id: add-backlog-stage-d553
stage: done
risk: high
deps: []
links: []
created: 2026-03-14T16:58:21Z
type: feature
priority: 2
tags: [pipeline, stage]
---
# Add backlog stage support to ticket pipeline

Add backlog as a recognized pipeline stage in the tk ticket system. This is the infrastructure change needed for Forge to implement a backlog → triage separation.

## Changes

**New stage: backlog**
- Add backlog as a valid pipeline stage, positioned before triage
- Default initial stage for ticket_create becomes backlog (currently triage)
- All ticket types start in backlog

**Pipeline enforcement**
- ticket_advance from backlog should be blocked (or gated differently — triage is a deliberate promotion, not a normal advance)
- Backlog tickets excluded from ticket_ready results by default
- Backlog tickets excluded from standard ticket_inbox results (or shown separately)

**Stage ordering**
- Full pipeline: backlog → triage → spec → design → implement → test → verify → done
- Type-dependent skipping still applies from triage onward
- backlog → triage transition is always explicit (never auto-skipped)

**Filtering**
- ticket_list should support filtering by/excluding backlog stage
- Consider a --include-backlog flag for commands that exclude it by default

## Acceptance Criteria

## Core Pipeline

1. When a ticket is created via `tk create`, the system shall assign `backlog` as the initial stage, verified by `go test ./internal/mcp/ -run TestCreateTicket`.

2. When a ticket is created via the `ticket_create` MCP tool, the system shall assign `backlog` as the initial stage, verified by `go test ./internal/mcp/ -run TestCreateTicket`.

3. When a ticket is created via the TUI form, the system shall assign `backlog` as the initial stage, verified by manual check: `tk ui`, press `c`, create ticket, confirm stage is `backlog`.

4. When `ValidateStage` is called with `"backlog"`, the system shall return nil (valid), verified by `go test ./pkg/ticket/ -run TestValidateStage`.

5. When `PipelineFor` is called for any ticket type, the system shall return a pipeline starting with `backlog` followed by `triage`, verified by `go test ./pkg/ticket/ -run TestPipelineFor`.

6. When `ticket_advance` is called on a ticket in `backlog` stage, the system shall advance it to `triage`, verified by `go test ./internal/mcp/ -run TestAdvanceFromBacklog`.

## Gates

7. When a ticket is advanced from `backlog` to `triage`, the system shall require `description_exists`, `priority_set`, and `risk_set` structural checks, verified by `go test ./pkg/ticket/ -run TestGates_BacklogToTriage`.

## Filtering / Exclusion

8. When `ticket_ready` MCP tool is called, the system shall exclude tickets in `backlog` stage, verified by `go test ./internal/mcp/ -run TestReadyExcludesBacklog`.

9. When `tk ready` CLI is called, the system shall exclude tickets in `backlog` stage, verified by `go test ./pkg/ticket/ -run TestIsReady`.

10. When `ticket_inbox` is called, the system shall exclude tickets in `backlog` stage, verified by `go test ./pkg/ticket/ -run TestInbox_ExcludesBacklog`.

11. When `tk ls` is called with no stage filter, the system shall exclude tickets in `backlog` stage (alongside `done`), verified by manual CLI check.

12. When `tk ls --stage backlog` is called, the system shall show only tickets in `backlog` stage, verified by manual CLI check.

13. When `ticket_list` MCP tool is called with no stage filter, the system shall exclude tickets in `backlog` stage, verified by `go test ./internal/mcp/ -run TestListExcludesBacklog`.

14. When `ticket_list` MCP tool is called with `stage: "backlog"`, the system shall return only backlog-stage tickets, verified by `go test ./internal/mcp/ -run TestListFilterBacklog`.

15. When `NextAction` is called on a ticket with stage `backlog`, the system shall return inert/no-action-needed, verified by `go test ./pkg/ticket/ -run TestNextAction_Backlog`.

## Display

16. When the TUI pipeline view is rendered, the system shall include a `backlog` column in the leftmost position, verified by manual check.

17. When `stageColors` is accessed for `StageBacklog` in the TUI, the system shall return gray/dim color, verified by code inspection.

18. When `colorizeStage` is called with `StageBacklog` in CLI table output, the system shall return gray styled string, verified by code inspection.

19. When `tk stats` is called, the system shall include `backlog` in the stage breakdown, verified by manual check.

## Infrastructure

20. When `AllStages()` is called, the system shall return `backlog` as the first element, verified by `go test ./pkg/ticket/ -run TestAllStages`.

21. When `DisplayStages()` is called, the system shall include `backlog`, verified by code inspection.

22. When `StatusToStage[StatusOpen]` is evaluated during migration, the system shall map to `triage` (not backlog), verified by `go test ./pkg/ticket/ -run TestMigrateTicket`.

23. When `tk pipeline` is run, the system shall include a `backlog` group header, verified by manual check.

24. When `tk next` iterates stage breakdowns, the system shall include `StageBacklog`, verified by code inspection.

25. When `PipelineDescription()` is called, the system shall show `backlog` as the first stage in each pipeline, verified by `go test ./pkg/ticket/ -run TestPipelineDescription`.

26. When existing pipeline tests assert pipeline lengths, the system shall pass with lengths incremented by 1 (feature: 8, bug: 6, chore: 4, epic: 5, task: 6), verified by `go test ./pkg/ticket/`.

27. When all tests are run, the system shall pass, verified by `go test ./...`.

## CLI Command

28. When `tk backlog` is called, the system shall list all tickets in `backlog` stage using standard table formatting, verified by manual CLI check.

## Stage Edit Support

29. When `tk edit <id> --stage <stage>` is called with a valid stage name, the system shall update the ticket's stage to the specified value (bypassing pipeline ordering), verified by manual CLI check.

30. When `tk edit <id> --stage <stage>` is called with an invalid stage name, the system shall return a validation error, verified by manual CLI check.

31. When `ticket_edit` MCP tool is called with a `stage` parameter containing a valid stage name, the system shall update the ticket's stage, verified by `go test ./internal/mcp/ -run TestEditStage`.

32. When a batch command `tk ls --stage triage -q | xargs -I{} tk edit {} --stage backlog` is run, all triage-stage tickets shall be moved to backlog, verified by manual CLI check.

## What Could Go Wrong

- **Test cascade**: Many tests hardcode `StageTriage` as first pipeline stage or assert specific pipeline lengths. All must be updated simultaneously. Mitigation: grep for `StageTriage` in test files and update each.
- **Hidden exclusion gaps**: Multiple places independently check `StageDone` to exclude closed tickets. Each needs auditing for backlog exclusion. Mitigation: grep for every `StageDone` exclusion pattern.
- **Downstream agent breakage**: MCP clients expecting triage post-create get backlog instead. Mitigation: Forge parallel update is planned.
- **Gate key mismatch**: `backlog>triage` gate key must match `GateKey()` format. Mitigation: test that `Gates(StageBacklog, StageTriage)` returns non-empty.
- **Pipeline ordering**: `StageIndex` shifts all indices +1. Mitigation: search for numeric stage index comparisons.
- **Stage edit bypasses gates**: Direct stage edit via `tk edit --stage` bypasses pipeline gates intentionally. This is a power-user escape hatch, not the normal flow.

## Scope

### In Scope
- `pkg/ticket/pipelines.json` — stage config, stage order, pipeline variants, gates
- `pkg/ticket/ticket.go` — `StageBacklog` constant
- `pkg/ticket/inbox.go` — exclusion from NextAction and Inbox
- `pkg/ticket/deps.go` — exclusion from IsReady/IsReadyOpen
- `cmd/create.go` — default stage change
- `cmd/edit.go` — add `--stage` flag
- `cmd/ls.go` — default exclusion
- `cmd/next.go` — stage display list
- `cmd/table.go` — colorizeStage
- `cmd/backlog.go` — new `tk backlog` command
- `cmd/root.go` — help text
- `internal/mcp/mcp.go` — default stage, list exclusion, schema description, stage edit parameter
- `internal/tui/tui.go` — default stage in create
- `internal/tui/pipeline.go` — stage colors
- `internal/tui/dashboard.go` — exclusion from buildItems
- All affected test files

### Out of Scope
- Forge orchestrator changes (parallel ticket)
- Backlog promotion UX beyond `tk advance`
- Backlog-specific TUI dashboard tab
- Bulk backlog operations beyond `tk edit --stage`
- Migration of existing triage tickets to backlog (manual via `tk edit --stage`)
- MCP equivalent of `tk backlog` (`ticket_list` with `stage: "backlog"` suffices)

## Design

## Architecture

Backlog is a pipeline-level stage inserted before triage in every pipeline variant. The single source of truth is `pipelines.json` — all stage ordering, validation, and pipeline queries derive from it. A Go constant `StageBacklog` provides type-safe references for filtering/display.

Two new structural gate checks (`priority_set`, `risk_set`) enforce that tickets have risk set and priority validated before promotion from backlog to triage. Combined with existing `description_exists`, this creates friction at the backlog→triage boundary.

Backlog tickets are excluded alongside `StageDone` in all default views: `tk ls`, `ticket_list`, `ticket_ready`, `ticket_inbox`, `IsReady`, TUI dashboard. Explicit `--stage backlog` or `tk backlog` shows them.

A `--stage` flag on `tk edit` and a `stage` parameter on `ticket_edit` MCP tool allow direct stage assignment (bypassing pipeline ordering), enabling batch migration of existing tickets.

## Implementation Plan (ordered)

### 1. `pkg/ticket/ticket.go` — Add constant
Add `StageBacklog Stage = "backlog"` before `StageTriage` in the Stage const block.

### 2. `pkg/ticket/pipelines.json` — Core config change
- Add `"backlog": {"role": "intake"}` to `stages`
- Prepend `"backlog"` to `stage_order`
- Prepend `"backlog"` to every pipeline variant for all types x all risk levels
- Add gate: `"backlog>triage": {"structural": ["description_exists", "priority_set", "risk_set"]}`

### 3. `pkg/ticket/gates.go` — New structural checks
- Add `checkPrioritySet(t *Ticket) error` — validates priority in range 0-4 (lightweight, always passes for valid tickets)
- Add `checkRiskSet(t *Ticket) error` — returns error if `t.Risk == ""`
- Register both in `structuralCheckFunc` switch and `structuralDescription`

### 4. `pkg/ticket/inbox.go` — Exclusion
- `NextAction`: add `StageBacklog` to early-return alongside `StageDone` -> returns `ActionReady` / "no action needed"
- `Inbox`: add `StageBacklog` to exclusion check alongside `StageDone`

### 5. `pkg/ticket/deps.go` — Exclusion
- `IsReady`: add `t.Stage == StageBacklog` to early false return
- `IsReadyOpen`: same

### 6. `cmd/create.go` — Default stage
Change `Stage: ticket.StageTriage` to `Stage: ticket.StageBacklog`

### 7. `cmd/edit.go` — Add `--stage` flag
- Add `--stage` string flag to the edit command
- Validate against `ticket.ValidateStage()` before applying
- Set `t.Stage` directly (bypasses pipeline advance/gates)
- Clear review state when stage changes (prevents stale approvals)

### 8. `internal/mcp/mcp.go` — Default stage + list exclusion + stage edit
- `registerCreate`: change default stage to `StageBacklog`
- `registerList`: add `StageBacklog` to default exclusion filter alongside `StageDone`
- Update `listArgs` stage description to include "backlog"
- `registerEdit`: add `stage` string parameter, validate with `ticket.ValidateStage()`, set `t.Stage` directly

### 9. `internal/tui/tui.go` — Default stage
Change `Stage: ticket.StageTriage` to `Stage: ticket.StageBacklog` in `handleCreateTicket`

### 10. `internal/tui/pipeline.go` — Stage color
Add `ticket.StageBacklog: "8"` to `stageColors` map (gray)

### 11. `internal/tui/dashboard.go` — Exclusion
Add `StageBacklog` to `buildItems` exclusion alongside `StageDone`

### 12. `cmd/table.go` — CLI color
Add `case ticket.StageBacklog:` returning `colorize(s, ansiGray)` in `colorizeStage`

### 13. `cmd/ls.go` — Default exclusion
Add `StageBacklog` to exclusion filter in `runLs` alongside `StageDone`

### 14. `cmd/next.go` — Stage list
Add `ticket.StageBacklog` to the stage iteration list at the beginning

### 15. `cmd/backlog.go` (new) — `tk backlog` command
New cobra command following `cmd/ready.go` pattern. Lists tickets where `Stage == StageBacklog` with standard table formatting.

### 16. `cmd/root.go` — Help text
- Add `backlog` to Viewing section
- Add `--stage` to edit command help
- Update stages line to start with `backlog ->`

### 17. Test updates
- `pkg/ticket/pipeline_test.go`: Update pipeline lengths (+1 each), stage indices (+1), PrevStage expectations, add backlog->triage NextStage test
- `pkg/ticket/gates_test.go`: Add backlog->triage gate tests (pass/fail cases for risk_set)
- `pkg/ticket/inbox_test.go`: Add NextAction_Backlog and Inbox_ExcludesBacklog tests
- `pkg/ticket/deps_test.go`: Add IsReady_BacklogNotReady test
- `internal/mcp/mcp_test.go`: Update TestCreateTicket assertion to "backlog", update createAndAdvanceTo helper, add list exclusion/inclusion tests, add TestEditStage test

### 18. CHANGELOG.md

## Key Decisions

- **StatusToStage[StatusOpen] -> triage (not backlog):** Legacy tickets represent previously triaged work. No migration needed.
- **priority_set is lightweight:** No way to distinguish "default 2" from "intentionally 2" without model change. Gate validates range only. Real friction from risk_set + description_exists.
- **tk pipeline shows backlog:** Pipeline view shows full pipeline. No exclusion.
- **No automatic migration:** Existing tickets stay at current stages. Batch migration available via `tk ls --stage triage -q | xargs -I{} tk edit {} --stage backlog`.
- **Stage edit bypasses gates:** Direct stage assignment via `tk edit --stage` is a power-user escape hatch. Normal flow uses `tk advance`.

## Rollback Strategy

1. Revert `pipelines.json` (remove backlog from all pipelines, stage_order)
2. Revert default stage in `create.go`, `mcp.go`, `tui.go`
3. Revert exclusion additions
4. Migration script for any tickets created at backlog during rollback window -> move to triage via `tk edit --stage triage`
5. `StageBacklog` constant, gate functions, and `--stage` flag can remain as they are independently useful

## Test Results

**2026-03-14:** 178 tests, 178 passed, 0 failed

**Full suite:** `go test ./... -count=1` — PASS (3 packages)

**Acceptance Criteria (automated):**
- [x] AC 1-2 — TestCreateTicket: backlog as initial stage
- [x] AC 5 — TestPipelineFor_AllTypes: pipelines start with backlog
- [x] AC 6 — TestAdvanceFromBacklog: backlog advances to triage
- [x] AC 7 — TestCheckGates_BacklogToTriage: 3 gate test cases pass
- [x] AC 8 — TestReadyExcludesBacklog: ticket_ready excludes backlog
- [x] AC 9 — TestIsReady_BacklogNotReady: IsReady/IsReadyOpen return false
- [x] AC 10 — TestInbox_ExcludesBacklog: Inbox excludes backlog
- [x] AC 13 — TestListExcludesBacklog: ticket_list excludes backlog by default
- [x] AC 14 — TestListFilterBacklog: ticket_list with stage:"backlog" works
- [x] AC 15 — TestNextAction_Backlog: returns inert/no-action-needed
- [x] AC 26 — TestPipelineLengths: feature:8, bug:6, chore:4, epic:5, task:6
- [x] AC 27 — Full test suite passes
- [x] AC 31 — TestEditStage: ticket_edit stage parameter works

**Manual verification needed:** AC 3, 11, 12, 16-19, 23-25, 28-30, 32

## Review Log

**2026-03-14T17:26:31Z [human:steve]**
APPROVED — 28 AC approved. Gate spec for backlog→triage confirmed: description + priority + risk.

**2026-03-14T17:40:28Z [agent:design-reviewer]**
APPROVED — All 28 AC covered. File paths verified. API references match codebase. Warnings: PrevStage/StageIndex test assertions will break implicitly (handle during implementation). priority_set gate is effectively a no-op since all tickets have valid priority — acceptable design decision.

**2026-03-14T17:59:16Z [human:steve]**
APPROVED — 32 AC approved. Design includes stage edit support (--stage flag on tk edit + ticket_edit MCP) for batch migration.

**2026-03-14T17:59:31Z [human:steve]**
APPROVED — Design review approved. 32 AC, 18 implementation steps, stage edit support included.

**2026-03-14T18:16:44Z [agent:code-reviewer]**
APPROVED — Approved after fix. Critical finding (review state not cleared on direct stage edit) resolved. BlockedTickets exclusion gap fixed. Style consistent, patterns match existing codebase.

**2026-03-14T18:16:45Z [agent:impl-reviewer]**
APPROVED — Approved after adding 9 missing test functions. All 32 AC covered. Design adherence verified. Review state clearing deviation resolved. AC 32 batch workflow uses tk query as alternative to missing -q flag.

**2026-03-14T18:18:31Z [agent:code-reviewer]**
APPROVED — Code review passed. Review state clearing and BlockedTickets exclusion fixed.

**2026-03-14T18:34:39Z [human:steve]**
APPROVED — All acceptance criteria verified. TUI dashboard tab redesign filed as separate ticket (redesign-tui-dashboard-d727). Batch migration uses tk query alternative.

## Notes

**2026-03-14T17:14:16Z**

## Triage

**Risk:** high — Touches pipeline stage ordering, gates, MCP tools (ticket_create, ticket_ready, ticket_inbox), CLI filtering. Changes default behavior for all ticket creation callers.

**Scope:** single ticket, but wide blast radius across layers (pipeline, gates, MCP, CLI, possibly TUI)

**Key decisions:**
- Default initial stage for ticket_create becomes backlog (option 2) (human)
- Forge will be updated in parallel to understand backlog as default (human)
- Semantic distinction: backlog = idea queue, triage = deliberate work promotion (human)
- backlog → triage is always an explicit promotion, never auto-skipped (auto)

**Session:** triage-add-backlog-stage-d553-20260314

**2026-03-14T17:25:28Z**

## Spec

**Scope:** 28 acceptance criteria across core pipeline, gates, filtering, display, infrastructure, and new CLI command. ~14 source files + test files.

**Decisions:**
- backlog→triage gate requires description_exists + priority_set + risk_set (human)
- type remains required at ticket creation, not deferred to triage (human)
- tk ls excludes backlog by default alongside done (human)
- New `tk backlog` CLI command added for consistency with `tk done`/`tk ready` (human)
- No MCP equivalent of `tk backlog` — ticket_list with stage filter suffices (human)

**Session:** spec-add-backlog-stage-d553-20260314

**2026-03-14T17:41:13Z**

## Design

**Approach:** Add backlog as first pipeline stage via pipelines.json config. Exclude from default views (ls, list, ready, inbox) alongside StageDone. New structural gates (priority_set, risk_set) for backlog→triage transition. New `tk backlog` CLI command.

**Files affected:**
- pkg/ticket/ticket.go, pipelines.json, gates.go, inbox.go, deps.go
- cmd/create.go, ls.go, next.go, table.go, root.go, backlog.go (new)
- internal/mcp/mcp.go
- internal/tui/tui.go, pipeline.go, dashboard.go
- Tests: pipeline_test.go, gates_test.go, inbox_test.go, deps_test.go, mcp_test.go

**Review:** agent:design-reviewer: approved — all 28 AC covered, file paths verified, API references match codebase

**Session:** design-add-backlog-stage-d553-20260314

**2026-03-14T18:17:05Z**

## Implement

**Changes:**
- pkg/ticket/ticket.go: added StageBacklog constant
- pkg/ticket/pipelines.json: backlog stage, stage_order, all pipeline variants, backlog>triage gate
- pkg/ticket/gates.go: checkPrioritySet, checkRiskSet functions
- pkg/ticket/config.go: structuralDescription entries for priority_set, risk_set
- pkg/ticket/inbox.go: StageBacklog exclusion in NextAction and Inbox
- pkg/ticket/deps.go: StageBacklog exclusion in IsReady, IsReadyOpen, BlockedTickets
- cmd/create.go: default stage → StageBacklog
- cmd/edit.go: updated --stage description, review state clearing on stage change
- cmd/ls.go: StageBacklog default exclusion
- cmd/next.go: StageBacklog in stage iteration list
- cmd/table.go: StageBacklog gray color
- cmd/backlog.go: NEW tk backlog command
- cmd/root.go: help text updated
- internal/mcp/mcp.go: default stage, list exclusion, stage edit parameter with review clearing
- internal/tui/tui.go: default stage → StageBacklog
- internal/tui/pipeline.go: StageBacklog gray color
- internal/tui/dashboard.go: StageBacklog exclusion in buildItems
- CHANGELOG.md, README.md: updated

**Tests added:**
- gates_test.go: TestCheckGates_BacklogToTriage (3 cases)
- inbox_test.go: TestNextAction_Backlog, TestInbox_ExcludesBacklog
- deps_test.go: TestIsReady_BacklogNotReady
- mcp_test.go: TestAdvanceFromBacklog, TestReadyExcludesBacklog, TestListExcludesBacklog, TestListFilterBacklog, TestEditStage

**Fixes from review:**
- Review state (t.Review) now cleared on direct stage edit (code-reviewer critical finding)
- BlockedTickets excludes StageBacklog for consistency (code-reviewer suggestion)

**Reviews:**
- code-reviewer: approved (after review state fix)
- impl-reviewer: approved (after adding 9 missing tests)

**Session:** implement-add-backlog-stage-d553-20260314
