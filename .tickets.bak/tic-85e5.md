---
id: tic-85e5
status: closed
deps: [tic-e581]
links: []
created: 2026-02-26T04:33:27Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Update stage propagation logic





Update PropagateStatus to work with stages. All children at done → parent advances to done. All children at test or later → parent advances to test (if applicable). Rename to PropagateStage, keep PropagateStatus as deprecated wrapper.
