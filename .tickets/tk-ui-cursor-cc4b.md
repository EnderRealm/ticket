---
7stage: implement
risk: low
deps: []
links: []
created: 2026-03-01T01:14:24Z
type: bug
priority: 1
branch: forge/tk-ui-cursor-cc4b
---
# 'tk ui' cursor highlighting on Priority is messed up

The cursor is one yellow bar and one white bar. Same for Stage cursor. But the Type cursor is two yellow bars. Should be consistent. Also not sure colors of priorites and stages match color choices on the dashboard. And types don't have colors. Just in general colors on the edit/view page are kind of messed up.

## Notes

**2026-03-15T21:04:13Z**

## Triage

**Risk:** low — Purely cosmetic fix isolated to internal/tui/ styling code. No data, API, or behavior changes.

**Scope:** Single task with three related sub-issues:
1. Inconsistent cursor bar colors (yellow+white on Priority/Stage vs yellow+yellow on Type)
2. Priority/stage colors may not match dashboard color choices
3. Types lack colors entirely

**Key decisions:**
- Risk level: low (human)
