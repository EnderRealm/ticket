---
id: better-navigation-long-736e
stage: done
risk: low
deps: []
links: []
created: 2026-03-15T06:08:15Z
type: feature
priority: 1
---
# Better navigation for long lists

In list views support mouse scroll and page up/page down

## Acceptance Criteria

1. `pgup`/`pgdn` in dashboard moves cursor by one page of visible rows
2. Mouse scroll wheel in dashboard moves cursor up/down
3. `tea.WithMouseCellMotion()` added to tea.Program options to enable mouse events

**Out of scope:**
- Pipeline view (unreachable, separate bug tk-ui-pipeline-3c52)
- Detail view (already has pgup/pgdn)
- Click-to-select
- b/f/space aliases

## Design

## Design

### File 1: `cmd/ui.go` (line 17)
Add `tea.WithMouseCellMotion()` option to `tea.NewProgram` to enable mouse events.

### File 2: `internal/tui/dashboard.go`

**Change 1: pgup/pgdn keys (in `update()` KeyMsg switch, ~line 226)**
Add cases for `pgup` and `pgdn` alongside existing `up`/`down`/`g`/`G`:
- `pgup`: `m.cursor -= m.visibleRows()`, clamp to 0, then `clampOffset()`
- `pgdn`: `m.cursor += m.visibleRows()`, clamp to `len(items)-1`, then `clampOffset()`

**Change 2: mouse scroll (in `update()`, new `tea.MouseMsg` case)**
Add a `tea.MouseMsg` type switch in the dashboard's `update()` method:
- `tea.MouseWheelUp`: move cursor up by 3 (or to 0), `clampOffset()`
- `tea.MouseWheelDown`: move cursor down by 3 (or to max), `clampOffset()`

No new fields, types, or dependencies.

## Test Results

All tests pass:\n- go test ./... — PASS (tui, mcp, pkg/ticket)\n- go build ./... — clean compile

## Review Log

**2026-03-15T06:25:06Z [human:steve]**
APPROVED — AC confirmed, scope narrowed to dashboard only

**2026-03-15T06:25:30Z [agent:ghost]**
APPROVED — Two files, three small changes. Follows existing patterns exactly. No risk.

**2026-03-15T06:26:35Z [agent:code-review]**
APPROVED — Two files, minimal changes following existing cursor/clamp patterns. Clean build, all tests pass.

**2026-03-15T06:26:40Z [agent:impl-review]**
APPROVED — All 3 AC covered: pgup/pgdn moves by visibleRows, mouse scroll moves cursor, WithMouseCellMotion enabled.

**2026-03-15T06:28:25Z [human:steve]**
APPROVED — Verified pgup/pgdn and mouse scroll in tk ui

## Notes

**2026-03-15T06:11:33Z**

## Triage

**Risk:** low — Isolated TUI input handling, no risk indicators. Bubbletea has built-in mouse/key event support.

**Scope:** Single task. Add mouse scroll and pgup/pgdn to dashboard and pipeline list views.

**Key decisions:**
- Risk level: low (human)

**2026-03-15T06:25:06Z**

## Spec

**Scope narrowed from triage:** Pipeline view is unreachable (filed tk-ui-pipeline-3c52), so dashboard only. No b/f/space aliases — just pgup/pgdn. (human)

**2026-03-15T06:26:35Z**

## Implementation

**Changes:**
1. `cmd/ui.go`: Added `tea.WithMouseCellMotion()` to tea.Program
2. `internal/tui/dashboard.go`: Added `pgup`/`pgdn` key cases (move cursor by visibleRows)
3. `internal/tui/dashboard.go`: Added `tea.MouseMsg` handler for wheel up/down (move cursor ±3)

All tests pass, clean build.
