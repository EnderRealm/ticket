---
id: tic-ab2c
status: closed
deps: []
links: []
created: 2026-02-22T22:01:56Z
type: task
priority: 2
assignee: Steve Macbeth
parent: tic-18e9
tags: [go-parity]
---
# Add stats command





stats command missing from Go version. Shows project health: status counts, type counts, priority distribution, open ticket age stats.

## Design

Files: cmd/stats.go
Implement cmd_stats matching bash output format:
- Status breakdown (open, in_progress, needs_testing, closed, TOTAL)
- Type breakdown (epic, feature, task, bug, chore)
- Priority breakdown (P0-P4)
- Open ticket stats: count, average age in days, oldest ticket (days + ID)
Age calculation: days since created field.

## Acceptance Criteria

tk stats output matches bash format. Counts are correct against real tickets.

## Notes

**2026-02-22T23:58:59Z**

Implemented cmd/stats.go matching bash output format. Status/type/priority breakdowns plus open ticket age stats. 5 test assertions added. 134/134 pass.
