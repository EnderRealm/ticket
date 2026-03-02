---
id: tk-ui-type-4515
stage: verify
status: open
deps: []
links: []
created: 2026-03-01T01:13:50Z
type: bug
priority: 0
---
# 'tk ui' TYPE column too short for 'chore'

The TYPE column on the 'tk ui' list pages truncates 'chore' to 'chor' to make it consistent with the size of the column, which is currently limited to four characters to match 'TYPE'. However, we should spend one extra character and make that column five characters so that we can fit 'chore' in without truncation.

## Implementation Notes

Widened the TYPE column from 4 to 5 characters in both TUI views:

- `internal/tui/dashboard.go`: header format `%-4s` → `%-5s`; row format `%-4s` → `%-5s`
- `internal/tui/pipeline.go`: card format `%-4s` → `%-5s`; `shortType(TypeChore)` now returns `"chore"` instead of `"chor"`

All other short-type abbreviations (`feat`, `task`, `epic`, `bug`) are ≤ 4 chars and display correctly within the 5-char column.
