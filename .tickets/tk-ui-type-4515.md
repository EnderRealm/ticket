---
id: tk-ui-type-4515
stage: done
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

Widened the TYPE column from 4 to 6 characters in both TUI views, giving all type labels a one-character padding margin:

- `internal/tui/dashboard.go`: header format `%-4s` → `%-6s`; row format `%-4s` → `%-6s`
- `internal/tui/pipeline.go`: card format `%-4s` → `%-6s`; `shortType(TypeChore)` now returns `"chore"` instead of `"chor"`

All short-type abbreviations now display without truncation within the 6-char column:

| Type    | Short  | Width |
|---------|--------|-------|
| feature | feat   | 4     |
| bug     | bug    | 3     |
| task    | task   | 4     |
| epic    | epic   | 4     |
| chore   | chore  | 5     |
