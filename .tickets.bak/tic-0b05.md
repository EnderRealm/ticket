---
id: tic-0b05
status: closed
deps: []
links: []
created: 2026-02-26T04:34:20Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-cd50
tags: [redesign, mcp]
---
# Update MCP tools for stage awareness




Update all existing MCP tool descriptions to reference stages instead of statuses. ticket_list: add stage filter, return stage/review in JSON. ticket_ready: stage-aware. ticket_show: return stage, review, conversations, pipeline position. ticket_create: set stage=triage. ticket_edit: support stage, review, conversation fields. Update ticket_workflow tool output with pipeline docs.
