---
id: tic-a639
status: closed
deps: [tic-9660]
links: []
created: 2026-02-22T22:02:38Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-18e9
tags: [go-parity]
---
# Add test coverage for untested capabilities






Audit found several capabilities that exist in Go but have no test-suite.sh coverage.

## Design

Files: test-suite.sh
Add assertions for:
- Partial ID matching (tk show with prefix/substring)
- dep tree --full flag
- closed --limit=N flag
- add-note via stdin (echo text | tk add-note ID)
- ls --parent=X filter
- ready/blocked/closed with -a (assignee) filter
- ready/blocked/closed with -T (tag) filter
- show with multiple IDs
- ls --group-by (once implemented)
Each gap needs at least one positive assertion.

## Acceptance Criteria

All listed capabilities have at least one test assertion in test-suite.sh. All pass.

## Notes

**2026-02-22T23:18:56Z**

Added 26 new assertions across 8 test sections: partial ID matching, dep tree --full, closed --limit, add-note via stdin, ls --parent, ready/blocked/closed -a/-T filters, show multiple IDs, ls --group-by. Also fixed ID generation collision bug (UnixNano + atomic counter) and added retry-on-collision in FileStore.Create. 129/129 pass.
