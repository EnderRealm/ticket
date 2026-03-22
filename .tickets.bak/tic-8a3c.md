---
id: tic-8a3c
status: closed
deps: []
links: []
created: 2026-02-26T04:34:30Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, testing]
---
# Write Phase 2 integration tests




Update test-suite.sh with all new commands. Integration tests for advance, review, skip, pipeline. Backward compat tests (old commands work with warnings). MCP tool tests via Go test harness. Workspace mode tests: multi-repo list, cross-repo deps, qualified IDs.

## Notes

**2026-02-26T06:02:18Z**

188/188 integration tests pass. All pipeline commands tested.
