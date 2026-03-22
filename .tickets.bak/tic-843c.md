---
id: tic-843c
status: closed
deps: [tic-5eb6]
links: []
created: 2026-02-26T04:33:18Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Add gate definitions in pkg/ticket/gates.go





New file pkg/ticket/gates.go. GateCheck struct and Gates() function returning preconditions for each stage transition. Gates: triage→spec/implement (description exists), spec→design (AC exists + review approved), design→implement (design+plan exist + review approved), implement→test (mandatory code-review + impl-review approved), test→verify (tests pass), verify→done (review approved). Risk-scaled gates: low=advisory only, normal=standard, high=mandatory design-review + 2 code reviewers, critical=all high gates + human-only implementation.
