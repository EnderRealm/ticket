---
id: t-afcf
status: closed
deps: [t-3236]
links: []
created: 2026-02-22T00:58:13Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# Dependency graph operations

Implement dependency tree traversal, cycle detection, blocked/ready checks, and parent/epic gating.

## Design

Files: pkg/ticket/deps.go
Approach:
- DepTree(store Store, id string, full bool) ([]DepNode, error): walk dependency graph, dedup by default, --full disables dedup
- FindCycles(store Store) ([][]string, error): detect cycles in open tickets using DFS with coloring
- IsBlocked(store Store, t *Ticket) bool: any dep has status != closed
- IsReady(store Store, t *Ticket) bool: not blocked AND passes parent gate
- Parent gate: children of epics only ready when epic is in_progress
- ready --open flag: bypasses parent gate, shows all unblocked open tickets regardless of epic status

## Acceptance Criteria

Dep tree, cycle detection, blocked/ready logic matches bash behavior. Parent gating works for epics.


## Notes

**2026-02-22T06:26:05Z**

Implemented DepTree, FindCycles (DFS with coloring), IsBlocked, IsReady (with parent chain gating), IsReadyOpen (bypass gating), BlockingDeps, ReadyTickets, BlockedTickets, AddDep, RemoveDep, AddLink, RemoveLink. 19 tests.
