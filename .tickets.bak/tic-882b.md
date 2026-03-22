---
id: tic-882b
status: closed
deps: [tic-4042, tic-a2cc]
links: []
created: 2026-02-26T04:33:44Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline, testing]
---
# Write Phase 1 tests






pipeline_test.go: pipeline definitions, NextStage, HasStage for all types. gates_test.go: gate checks for every transition, mandatory vs advisory. workflow_test.go: Advance, Skip, SetReview tests. migrate_test.go: all status→stage mappings, idempotency, round-trip. format_test.go: new fields, backward compat, review log round-trip. inbox_test.go: inbox derivation, conversational stage handling, project grouping, sort order.
