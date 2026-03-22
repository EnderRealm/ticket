---
id: t-4b4b
status: closed
deps: [t-3236, t-a5bc]
links: []
created: 2026-02-22T00:58:27Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# CLI commands: create, show, edit, delete, status shortcuts

Implement the core CRUD CLI commands via cobra with exact same flags as bash version.

## Design

Files: cmd/create.go, cmd/show.go, cmd/edit.go, cmd/delete.go, cmd/status.go
Approach:
- create: title arg, -d, --design, --acceptance, -t, -p, -a, --external-ref, --parent, --tags flags. Output ticket frontmatter on success.
- show: id arg (partial match). Display full ticket markdown. Support multiple IDs (t-5456).
- edit: id arg + same flags as create for field updates. --title for title change.
- delete: id arg(s). Remove file(s).
- status: id + status arg. Shortcut for edit -s.
- start/close/reopen: convenience commands wrapping status changes.
- All commands support --json flag for structured output.
- Error messages match bash version where possible.

## Acceptance Criteria

create/show/edit/delete/status/start/close/reopen work with same flags as bash. --json outputs structured JSON.


## Notes

**2026-02-22T07:47:08Z**

Implemented create, show, edit, delete, status, start, close, reopen commands. All flags match bash version. Show displays blockers/blocking/children/linked sections. Edit propagates terminal status changes. Multi-ID show supported.
