---
id: tic-79f1
status: closed
deps: [tic-5eb6]
links: []
created: 2026-02-26T04:33:36Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Add validation for stage pipeline fields





Update Validate() to accept either stage or status (not neither). Add ValidateStage() to check stage is valid for ticket type. Add ValidateGates() to check all gate preconditions without advancing.
