---
id: tk-ui-current-27f0
stage: done
risk: low
deps: []
links: []
created: 2026-03-15T01:51:29Z
type: feature
priority: 1
---
# 'tk ui' current (t)ype filter should be visible

The current (t)ype filter should be visible

## Acceptance Criteria

1. When a type filter is active in dashboard view, the footer displays the current filter (e.g., `type: feature`)
2. When no type filter is active in dashboard view, the footer displays `all types`
3. Type filter display coexists with text search filter (both can be visible)
4. Behavior matches the existing pipeline view pattern

**Out of scope:**
- Pipeline view (already works)
- Changing the filter cycling behavior

## Design

## Design

**File:** `internal/tui/dashboard.go`, View method (lines 320-331)

**Change:** Replace the single footer line with a two-line footer matching the pipeline view pattern:
1. **Filter line** — shows type filter state + text search state
2. **Help bar** — existing help text

**Logic (matching pipeline.go lines 267-282):**
- If `filterActive`: show `/ <text>█` (text input mode — same as current)
- Else if `filterText != ""`: show `filter: <text>  (/ to edit, esc clears)` (same as current)
- Else: show type filter state (`type: <name>` or `all types`) + hint `(t type, / search)`

Delete confirmation prompt remains unchanged (takes priority over everything).

**No new fields or dependencies.** Uses existing `m.typeFilter` and `filterStyle`.

## Test Results

All tests pass:\n- go test ./... — PASS (tui, mcp, pkg/ticket)\n- go build ./... — clean compile, no errors

## Review Log

**2026-03-15T06:06:32Z [human:steve]**
APPROVED — AC confirmed by Steve

**2026-03-15T06:06:53Z [agent:ghost]**
APPROVED — Trivial single-file change mirroring existing pipeline pattern. No architectural risk.

**2026-03-15T06:07:44Z [agent:code-review]**
APPROVED — Single file change, mirrors existing pipeline pattern exactly. No new dependencies. Build and tests pass.

**2026-03-15T06:07:49Z [agent:impl-review]**
APPROVED — All 4 AC covered: type filter displayed when active, "all types" when inactive, coexists with text filter, matches pipeline pattern. Height calculation updated correctly.

**2026-03-15T06:09:09Z [human:steve]**
APPROVED — Verified visually in tk ui

## Notes

**2026-03-15T06:02:50Z**

## Triage

**Risk:** low — Isolated TUI display change, no risk indicators.

**Scope:** Single task. Add visual indicator for the active type filter in `tk ui`.

**Key decisions:**
- Risk level: low (human)

**2026-03-15T06:06:25Z**

## Spec

**AC agreed with Steve (human). Dashboard view needs type filter display to match pipeline view pattern.**

**2026-03-15T06:07:37Z**

## Implementation

Modified `internal/tui/dashboard.go`:
1. Replaced single footer line with two-line footer (filter line + help bar), matching pipeline view pattern
2. Updated `visibleRows()` to reserve the extra line (height - 4 instead of 3)
3. Removed redundant filter hints from help bar since they're now on the filter line

All tests pass, full build clean.
