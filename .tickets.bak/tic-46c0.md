---
id: tic-46c0
status: closed
deps: []
links: []
created: 2026-02-26T04:32:51Z
type: epic
priority: 0
assignee: Steve Macbeth
tags: [redesign, pipeline]
---
# Phase 1: Stage Pipeline Core




Replace the status enum with stage + type-dependent pipelines in the core library. New types (Stage, ReviewState, RiskLevel), pipeline definitions, gate checks, advance/skip/review logic, migration, inbox/next derivation. Pure pkg/ticket/ library work — no CLI or MCP changes.
