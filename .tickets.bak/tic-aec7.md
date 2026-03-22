---
id: tic-aec7
status: closed
deps: []
links: []
created: 2026-02-26T04:34:17Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, cli]
---
# Update existing CLI commands for stage awareness




Update tk ls: add --stage filter, default grouping to pipeline view, support --group-by stage. Update tk ready: stage-aware readiness. Update tk show: display stage, review state, pipeline position, conversations, review log. Update tk create: new tickets start at triage stage, accept --stage for testing. Update tk edit: add --stage, --review, --add-conversation flags. Deprecate tk status with warning.
