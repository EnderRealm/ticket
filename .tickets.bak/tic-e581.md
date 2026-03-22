---
id: tic-e581
status: closed
deps: [tic-843c, tic-2602]
links: []
created: 2026-02-26T04:33:26Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Implement advance/skip/review workflow logic in pkg/ticket/workflow.go






New or extended file pkg/ticket/workflow.go. Implement Advance() with gate checking, AdvanceOptions (SkipTo, Reason, Force), SetReview() to record review verdicts, Skip() for non-adjacent stage jumps with audit trail. AdvanceResult struct with From, To, Skipped, GateFailed, Propagation fields.
