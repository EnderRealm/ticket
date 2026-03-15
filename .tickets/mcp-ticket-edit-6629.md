---
id: mcp-ticket-edit-6629
stage: done
risk: normal
deps: []
links: []
created: 2026-03-15T05:06:25Z
type: bug
priority: 1
tags: [mcp, backlog]
skipped: [test, verify]
---
# MCP ticket_edit fails on backlog-stage tickets: invalid stage backlog

The MCP ticket_edit tool rejects edits to tickets at the backlog stage with: "invalid stage backlog: not defined in pipeline configuration." The tk CLI handles the same edit fine.

## Reproduction

```
# MCP (fails)
ticket_edit: { id: "verify-stage-during-8a9b", risk: "high" }
→ "failed to update ticket: update: invalid stage backlog: not defined in pipeline configuration"

# CLI (works)
tk edit verify-stage-during-8a9b --risk high
→ Updated
```

## Likely Cause

The MCP server validates the ticket's current stage against the pipeline config before applying the edit. The pipeline config the MCP server resolves may not include backlog — either falling back to a built-in default that predates backlog, or resolving the pipeline differently than the CLI.

The tk CLI (v4.1.1) has backlog in its built-in default and works fine. The MCP server code path diverges somewhere in stage validation.

## Impact

Cannot use MCP tools on any backlog ticket in repos without a local .tickets/pipelines.json. All MCP-based operations (ticket_edit, ticket_advance, etc.) may be affected.

## Notes

**2026-03-15T05:08:35Z**

## Triage

**Risk:** normal — touches MCP public interface but likely isolated to stage validation logic divergence between CLI and MCP server

**Scope:** single task

**Key decisions:**
- Root cause is MCP server resolving pipeline config differently than CLI, rejecting backlog as a valid stage (auto)
- Bug type: CLI/MCP parity issue (auto)

**2026-03-15T05:24:53Z**

## Investigation

**Root cause:** The embedded `pipelines.json` in the running MCP server binary did not include `backlog` as a valid stage. The `backlog` stage was added in commit 4f67ed1 (2026-03-14). Any `tk serve` process started from a binary built before that commit would reject backlog-stage tickets during `store.Update()` -> `t.Validate()` -> `ValidateStage()`.

**Current state:** Bug is not reproducible with current code. The embedded `pipelines.json` includes backlog. The installed binary (f589888) was built after the backlog commit. Both CLI and MCP use the same validation path through `isValidStage()` which checks `loadedConfig.Stages`.

**Fix:** Added regression test `TestEditBacklogTicketNonStageField` in `internal/mcp/mcp_test.go` that verifies editing a non-stage field (risk) on a backlog-stage ticket succeeds via MCP. This prevents future regressions if someone removes backlog from the pipeline config.

**Decision:** No code change needed beyond the regression test. The fix was the addition of backlog to `pipelines.json` in 4f67ed1. (auto)

**2026-03-15T05:28:23Z**

Closing as cannot duplicate. Bug was caused by stale MCP server binary predating backlog stage addition (4f67ed1). Current code and installed binary both include backlog. Added regression test.
