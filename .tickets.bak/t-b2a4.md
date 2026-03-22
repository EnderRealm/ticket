---
id: t-b2a4
status: closed
deps: []
links: []
created: 2026-02-22T03:44:23Z
type: feature
priority: 4
assignee: Steve Macbeth
---
# 'tk ready' needs better organization









Needs to be better organized to provide more useful information both to the human user and to the agent. Perhaps we need an agentic 'tk ready-agent' and a human 'tk ready'. For the human the tickets should be prioritized in this order:

needs testing
in progress (with dependencies)
open and unblocked

However for an agent 'tk ready-agent' it should not include 'needs testing' which is a human task.

## Notes

**2026-02-26T06:21:31Z**

Addressed by inbox/next commands: inbox surfaces human-attention items by action kind (human-review, agent-work, etc.), next shows per-project breakdown.
