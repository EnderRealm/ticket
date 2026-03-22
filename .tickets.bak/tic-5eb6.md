---
id: tic-5eb6
status: closed
deps: [tic-f258]
links: []
created: 2026-02-26T04:33:12Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Add type-dependent pipeline definitions in pkg/ticket/pipeline.go





New file pkg/ticket/pipeline.go. Define Pipelines map (feature: 7 stages, bug: 5, chore: 3, epic: 4, task: 5). Implement NextStage(), PrevStage(), HasStage(), StageIndex() functions.
