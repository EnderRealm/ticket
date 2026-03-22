---
id: json-envelope-standard-c7bf
stage: backlog
risk: normal
deps: []
links: []
created: 2026-03-22T04:01:18Z
type: task
priority: 3
parent: migrate-tk-storage-eb6c
tags: [architecture, tkt-port]
---
# JSON envelope standard for all CLI output

Wrap all `--json` CLI output in a standard `{meta, data}` envelope. Currently tk's JSON output varies by command — some return raw arrays, some return objects. A consistent envelope makes output self-documenting and debuggable.

**Envelope format:**
```json
{
  "meta": {
    "command": "ls",
    "project": "my-project",
    "generated_at": "2026-03-21T00:00:00Z",
    "version": "v2"
  },
  "data": { ... }
}
```

**What to build:**
- `jsonEnvelope` and `jsonMeta` types
- `emitJSON(command, data)` helper that resolves project name, fills timestamp, wraps in envelope
- Retrofit all existing `--json` outputs to use the envelope
- Add `--json` support to new commands (dashboard, progress, lifecycle, context)

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Envelope types | `internal/cli/json_output.go:14-24` | `jsonEnvelope{Meta, Data}`, `jsonMeta{Command, Project, GeneratedAt, Version}` |
| Emit function | `internal/cli/json_output.go:26-52` | `emitJSON()` — resolves project, builds envelope, marshals to stdout |
| Ticket to map | `internal/engine/helpers.go:110-152` | `TicketToMap()` — snake_case keys, includes custom sections |
| Summary to map | `internal/engine/helpers.go:154-163` | `TicketSummaryToMap()` — id, title, status, type, priority |

**Adaptation notes:**
- tk already has `--json` on many commands — this is a breaking change to their output format (adds envelope wrapper). Consider a `--json-v2` flag during transition, or just bump and document.
- MCP tool output should NOT use the envelope (tkt doesn't either) — envelope is CLI-only
- Version field set to "v2" to match tkt convention
