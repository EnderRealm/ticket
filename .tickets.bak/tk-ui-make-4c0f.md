---
id: tk-ui-make-4c0f
stage: done
status: open
deps: []
links: []
created: 2026-03-05T03:16:05Z
type: bug
priority: 0
---
# 'tk ui' make sure the id column is wide enough for the widest ticket id












The dashboard view in `tk ui` hardcodes the ID column width to 24 characters via `fmt.Sprintf("%-24s", t.ID)`. This wastes space when all IDs are short and misaligns when IDs exceed 24 chars. The fix is to dynamically compute the max ID width from the current item set and use that for both header and row formatting.

## Acceptance Criteria

1. ID column width in dashboard view is computed from the widest ticket ID in the current view\n2. Header and row ID columns align correctly regardless of ID length\n3. No hardcoded ID column width remains in dashboard rendering

## Test Results

All tests pass (`go test ./... -count=1`):\n- `internal/mcp` — PASS\n- `internal/tui` — PASS\n- `pkg/ticket` — PASS

## Review Log

**2026-03-05T04:35:58Z [agent:ghost]**
APPROVED — Simple fix: replaced hardcoded %-24s format with dynamic %-*s computed from max ID length across items. Builds clean, tests pass.

**2026-03-05T04:36:29Z [agent:code-review]**
APPROVED — Straightforward fix. Dynamic width computation from items replaces hardcoded constant. No issues.

**2026-03-05T04:36:30Z [agent:impl-review]**
APPROVED — Implementation matches acceptance criteria. Header and rows use same dynamic idWidth. Min width of 2 ensures header label fits.

**2026-03-05T04:41:12Z [human:steve]**
APPROVED — Verified — committed and pushed.

## Notes

**2026-03-05T04:36:42Z**

## Test Results\n\nAll tests pass (`go test ./... -count=1`):\n- `internal/mcp` — PASS\n- `internal/tui` — PASS\n- `pkg/ticket` — PASS
