---
id: mutation-journal-source-6af8
stage: backlog
risk: normal
deps: []
links: []
created: 2026-03-22T03:59:54Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [architecture, journal, tkt-port]
---
# Mutation journal with source attribution

Add an append-only mutation log that records every ticket change with source attribution. This creates a complete audit trail independent of git history — you can see who (human, claude, codex) changed what, when.

**What to build:**

1. **MutationEntry struct** — timestamp, ticket_id, operation, source, fields_changed
2. **AppendMutationLog function** — appends one JSON line to `~/.tk/state/<project>/mutations.jsonl`
3. **Instrument all write paths** — every CLI command and MCP tool that creates, edits, deletes, or modifies deps/links must call AppendMutationLog
4. **Source parameter on MCP writes** — all MCP write tools already accept a source param in tk's review system; extend this to all mutations

**Operations to log:** create, edit, add-note, delete, dep, undep, link, unlink, advance, skip, review

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| MutationEntry type | `internal/engine/types.go:23-30` | Timestamp, TicketID, Operation, Source, FieldsChanged |
| Append function | `internal/engine/journal.go:83-105` | `AppendMutationLog()` — auto-fills timestamp, O_APPEND write |
| Mutation path | `internal/engine/paths.go:18-25` | `MutationLogPath()` → `~/.tkt/state/<project>/mutations.jsonl` |
| CLI instrumentation | `internal/cli/core_commands.go:146,259,313` | create/edit/delete call AppendMutationLog |
| CLI deps | `internal/cli/deps_query_commands.go:55,107,178,235` | dep/undep/link/unlink call AppendMutationLog |
| MCP instrumentation | `internal/mcp/write_tools.go:87,163,208,245,286,309,333,370` | All 8 write tools call AppendMutationLog |

**Adaptation notes:**
- tk has more operations than tkt (advance, skip, review, revert) — log all of these too
- tk's MCP write tools use a `source` concept in reviews already — generalize it
- The mutation log complements tk's existing review records — reviews track approvals, mutations track edits
