---
id: rename-tk-closed-bda0
stage: done
risk: low
deps: []
links: []
created: 2026-03-12T19:06:20Z
type: bug
priority: 2
branch: forge/rename-tk-closed-bda0
run_id: orch-mmrh87ut-vyssq2
---
# Rename 'tk closed' to 'tk done'

The CLI command `tk closed` should be renamed to `tk done` to match the pipeline stage terminology. The final pipeline stage is called \"done\" but the command to list completed tickets is called \"closed\", creating an inconsistency.

## Review Log

**2026-03-15T18:49:03Z [human:tui]**
APPROVED — Looks good!

## Notes

**2026-03-15T07:39:09Z**

## Triage

**Risk:** low — Isolated CLI command rename, no risk indicators. Touches ~5 files in cmd/ and pkg/ticket/, plus README docs.

**Scope:** Single task

**Key decisions:**
- Risk level: low (human)

**Session:** triage-rename-tk-closed-bda0

**2026-03-15T08:13:59Z**

### Agent: Grimnar (surgical-coder) during implement @ 2026-03-15T08:13:59.808Z
### #####################################################################

## Summary

Renamed the `tk closed` command to `tk done` to match pipeline stage terminology.

### Changes

| File | Change |
|------|--------|
| `cmd/closed.go` | **Deleted** — old command file |
| `cmd/done.go` | **Created** — renamed command (`closedCmd`→`doneCmd`, `runClosed`→`runDone`, Use: `"done"`, Short: `"Show recently done tickets"`, local var `closed`→`done`) |
| `cmd/root.go` | Updated help text: command listing (line 28) and filter flags section (line 94) |
| `README.md` | Updated command reference (line 90) |

### Not changed (intentionally)
- `pkg/ticket/ticket.go` — `StatusClosed` is a legacy status value, not the CLI command
- `pkg/ticket/deps.go`, `cmd/timeline.go`, `cmd/migrate.go`, `internal/mcp/mcp.go` — use "closed" to describe ticket state concepts, not the CLI command

### Verification
- **Build:** PASS
- **Vet:** PASS
- **Tests:** 3/3 packages passed

### #####################################################################

**2026-03-15T08:14:01Z**

**Dispatch: implement**
- **Grimnar** (generator): approved — 92s, $0.4739

**2026-03-15T08:14:04Z**

### Agent: Orchestrator (orchestrator) during verify @ 2026-03-15T08:14:04.385Z
### #####################################################################

PR created: https://github.com/EnderRealm/ticket/pull/6

### #####################################################################

**2026-03-15T08:14:04Z**

## Awaiting Human Input

**Stage:** verify
**Action needed:** Stage verify requires human review
**PR:** https://github.com/EnderRealm/ticket/pull/6
**Run:** orch-mmrh87ut-vyssq2
