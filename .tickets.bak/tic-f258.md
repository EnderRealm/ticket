---
id: tic-f258
status: closed
deps: []
links: []
created: 2026-02-26T04:33:10Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Add Stage, ReviewState, RiskLevel types to pkg/ticket/ticket.go




Add new type definitions: Stage (triage/spec/design/implement/test/verify/done), ReviewState (none/pending/approved/rejected), RiskLevel (low/normal/high/critical), ReviewRecord struct, WaitingOn struct. These are the foundational types for the pipeline system.
