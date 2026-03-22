---
id: tk-ui-type-4515
stage: done
status: open
review: approved
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

## Review Log

**2026-03-02T08:32:06Z [agent:impl-reviewer]**
ALL TYPE LABELS NOW DISPLAY CLEANLY: `FEAT` (4), `BUG` (3), `TASK` (4), `EPIC` (4), AND `CHORE` (5) ALL FIT WITHIN THE 6-CHAR COLUMN WITH AT LEAST ONE TRAILING SPACE FOR BREATHING ROOM. — giving all type labels a one-character padding margin.

**2026-03-02T08:35:01Z [agent:code-reviewer]**
APPROVED — Now I have everything I need to do a thorough code review. Let me compile it:

---

## Code Review

**Ticket:** tk-ui-type-4515 — TYPE column too short for 'chore'
**Verdict:** ✅ APPROVED

---

### Summary of Changes

Three commits collectively fix the issue. The **net diff** on source files is:

| File | Change |
|---|---|
| `internal/tui/dashboard.go` | Header `%-5s` → `%-6s`; row `%-5s` → `%-6s` for TYPE column |
| `internal/tui/pipeline.go` | Card `%-5s` → `%-6s`; `shortType(TypeChore)` `"chor"` → `"chore"` |

---

### Findings

**Critical:** None.

**Suggestions:**

- `pipeline.go:354–368` / `dashboard.go:280` — The column width `6` appears as a magic literal in three independent `fmt.Sprintf` calls (header, dashboard row, pipeline card). If a future type abbreviation ever exceeds 5 chars, all three would need updating again. A named constant (e.g., `const typeColWidth = 6`) would make this a single-point-of-truth change. That said, `%-3s`, `%-10s`, and `%-24s` elsewhere in the same format strings are all magic literals too — so this is perfectly in keeping with the existing codebase style. Low priority.

**Positive:**

- `pipeline.go:354–368` — `shortType` is a clean, exhaustive switch. Good that it has a `default` fallback returning the raw type string rather than panicking or silently truncating.
- Both views (dashboard **and** pipeline) were updated in lockstep. A common failure mode is fixing the visible one and missing the other.
- Header format string and row format string use the same width (`%-6s`), so the header and data columns stay aligned. This is the right approach — applying the width to both the header `fmt.Sprintf` and the per-row `fmt.Sprintf`.

---

### Security

No security concerns. This is purely presentational formatting with no input handling, data access, or external I/O.

---

### Summary

A tight, surgical fix. Three commits reflect a reasonable iteration (workaround → fix → polish), and the final state of the code is correct and consistent. The column is now wide enough for all current type abbreviations (`feat` 4, `bug` 3, `task` 4, `epic` 4, `chore` 5) with one character of breathing room.
