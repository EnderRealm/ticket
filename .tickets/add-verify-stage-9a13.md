---
id: add-verify-stage-9a13
stage: triage
risk: normal
deps: []
links: []
created: 2026-03-14T21:09:36Z
type: bug
priority: 1
tags: [pipeline]
---
# Add verify stage to all pipeline variants

Low-risk pipelines skip verify, so the orchestrator pushes code but never creates a PR. Code is unreachable.

## Fix

Add verify to every pipeline variant in pipelines.json. Every type x every risk level should include verify before done.

Current missing variants: any pipeline that goes directly from implement (or test) to done without verify.

## Related

Forge verify skill will scale behavior based on risk level (separate ticket).
