---
id: migrate-tk-storage-eb6c
stage: backlog
risk: normal
deps: []
links: []
created: 2026-03-22T03:19:29Z
type: epic
priority: 2
tags: [architecture, tk, storage, tkt-port]
---
# Port tkt operational intelligence features to tk

Port the best of tkt's operational intelligence layer into tk while preserving tk's pipeline enforcement, gate system, and Forge agent orchestration.

**Source codebase:** `~/code/tkt` (Go 1.25.0, ~16,700 LOC)
**Target codebase:** `~/code/ticket` (Go 1.25.6, ~10,700 LOC)

## What we're porting

1. **Central store** — tickets in `~/.tickets/<project>/` instead of `.tickets/` in each repo
2. **Commit journal** — background daemon links git commits to tickets, tracks LOC/files/duration
3. **Mutation journal** — audit trail of all ticket edits with source attribution
4. **Precomputed views** — dashboard, progress, lifecycle, context, enhanced epic-view
5. **Schema preservation** — round-trip unknown YAML frontmatter fields
6. **JSON envelope standard** — `{meta, data}` wrapper on all JSON output

## What we're keeping from tk (not changing)

- 7-stage pipeline system with type-dependent pipelines
- Gate enforcement with risk scaling
- Structured review system with reviewer identity and verdicts
- Forge agent orchestration (skills, specialist agents)
- Inbox / next-action derivation
- Conversational vs autonomous stage designation

## Original motivation

Flat markdown files stored inside each project repo create sync headaches when multiple systems need to read/write tickets. Git-as-sync requires stash/pull/push cycles, service-dir clones, worktree file copies, and debounce timers — all with edge cases.

**Current pain:**
- Service directory clones go stale (ensureServiceDir never pulls)
- Orchestrator leaves orphaned unstaged changes that block future pulls
- Worktree file sync copies ticket state between branches
- Multiple repos mean multiple sources of truth

## Architecture principle

tkt's operational data layer bolted onto tk's workflow enforcement engine. Same markdown+YAML file format, same MCP server pattern, same TUI framework. The systems complement — no conflicts.

**What this doesn't solve (acceptable risk):**
- Concurrent writers (CLI, MCP server) can still race on the same git repo. In practice this is rare — pipeline and human edits rarely hit the same ticket simultaneously. If this becomes a real problem, SQLite can be layered in later.

## Key tkt reference files

All paths relative to `~/code/tkt/`:

| Area | Files |
|------|-------|
| Storage config | `internal/engine/paths.go`, `internal/project/config.go`, `internal/cli/init_config_commands.go` |
| Migration | `internal/cli/migrate_recompute_commands.go` |
| Daemon & sync | `internal/cli/watch_commands.go` |
| Journal types | `internal/engine/types.go`, `internal/engine/journal.go` |
| Journal analysis | `internal/journal/entry.go`, `internal/journal/lifecycle.go` |
| Precomputed views | `internal/cli/precomputed_views.go`, `internal/cli/context_command.go` |
| Schema preservation | `internal/ticket/model.go`, `internal/ticket/frontmatter.go` |
| JSON envelope | `internal/cli/json_output.go` |
| Helpers | `internal/engine/helpers.go` |
