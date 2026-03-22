---
id: tic-c930
status: closed
deps: [tic-2602]
links: []
created: 2026-02-26T04:33:35Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Update format.go for new fields and Review Log parsing





Update Serialize: write stage instead of status, write review/conversations/skipped/risk if non-empty. Update Parse: read both stage and status, auto-migrate in memory if only status present. Add Review Log section parsing — extract ReviewRecord structs from ## Review Log markdown section. Canonical format for review entries.
