---
id: tk-ready-ls-554b
stage: done
risk: low
deps: []
links: []
created: 2026-03-12T04:18:24Z
type: feature
priority: 1
---
# 'tk ready|ls' tidy up columns in output commands

Tidy up the column output for `tk ls` and `tk ready` CLI commands.

Current problems:
1. Header says "STATUS" but prints stage — rename to "STAGE"
2. ID column is fixed at 9 chars but actual IDs are 20-30 chars — alignment breaks
3. When `tk ls` groups by stage (default), the stage column is redundant
4. All column widths are hardcoded — no adaptation to actual data

Changes:
- Rename STATUS column to STAGE
- Dynamic column widths computed from actual data so columns line up
- Drop redundant columns contextually (e.g., stage when grouped by stage, type when grouped by type)
- Add color to improve terminal visibility
- No truncation of IDs

## Acceptance Criteria

### AC1: Column header rename
- WHEN `tk ls` or `tk ready` prints output THEN the column previously labeled "STATUS" SHALL be labeled "STAGE"
- **Verify:** Run `tk ls`; assert header line contains "STAGE" and does not contain "STATUS"

### AC2: Dynamic column widths
- WHEN `tk ls` or `tk ready` prints a table THEN each column width SHALL equal `max(len(header), max(len(data_values)))` plus a gutter of at least 2 spaces
- Columns in every data row SHALL start at the same character offset as the corresponding column in the header row
- The TITLE column is always the last column and SHALL have no trailing padding
- **Verify:** Create tickets with known IDs of varying length; capture output; assert all rows have columns starting at identical offsets

### AC3: Redundant column suppression
- WHEN `tk ls` groups by workflow (the default) or by pipeline (`--group-by=pipeline`) THEN the STAGE column SHALL be omitted — EXCEPT in the "Blocked" group where stage context is useful, STAGE SHALL be retained
- WHEN `tk ls` groups by type (`--group-by=type`) THEN the TYPE column SHALL be omitted
- WHEN `tk ls` groups by priority (`--group-by=priority`) THEN the P column SHALL be omitted
- WHEN `tk ls --flat` or `tk ready` prints output THEN all columns SHALL be shown
- **Verify:** Run `tk ls` (default grouping); assert header within stage groups does not contain "STAGE". Run `tk ls --flat`; assert header contains "STAGE".

### AC4: Color output
- Column headers SHALL be rendered in bold
- Priority P0 SHALL be red, P1 SHALL be yellow, P2+ SHALL use default terminal color
- Group headers (`=== name ===`) SHALL be rendered in bold cyan
- Color output SHALL be disabled when stdout is not a TTY (piped/redirected output) — no ANSI escape sequences in non-TTY output
- **Verify (TTY):** Visual inspection of colored output in terminal
- **Verify (non-TTY):** Pipe `tk ls` to a file; assert file contains zero ANSI escape sequences

### AC5: No ID truncation
- WHEN any ticket ID appears in the output THEN it SHALL be printed in full, never truncated
- Note: Explicit guard on top of AC2's dynamic widths
- **Verify:** Run `tk ls` with a ticket whose ID is 25+ chars; assert the full ID appears in output

### AC6: Existing tests pass
- All existing tests in `go test ./...` SHALL continue to pass
- Note: No existing tests cover cmd output formatting. AC1-AC5 are verified by their individual methods above.

## Design

### Approach

Replace the fixed-width `printHeader()`/`printRow()` functions with a `tableWriter` struct in a new file `cmd/table.go`. The writer collects tickets, computes column widths from actual data, supports column suppression, and renders with ANSI color when stdout is a TTY.

### New file: `cmd/table.go`

**tableWriter struct:**
- Holds a slice of `column` definitions (header name, value extractor function, computed width)
- Constructor `newTableWriter(skip ...string)` builds the column set, excluding named columns
- `Print(tickets)` computes widths then renders header + rows

**Column definitions (in order):**
1. `ID` — `t.ID`
2. `P` — `fmt.Sprintf("P%d", t.Priority)`
3. `TYPE` — `string(t.Type)`
4. `STAGE` — `string(t.Stage)`
5. `TITLE` — `t.Title` + optional dep suffix (suffix appended at render time, no width calc needed since TITLE is last column with no padding)

**Width computation:**
- For each non-TITLE column: `width = max(len(header), max(len(values)))`
- Gutter: 2 spaces between columns (added during render, not stored in width)
- TITLE column: no width computation, no padding (always last)

**Color (ANSI):**
- TTY detection: Follow existing pattern from `timeline.go` — `os.Stdout.Stat()` with `ModeCharDevice` check + `os.Getenv("NO_COLOR") == ""`
- No new dependencies needed (no `go-isatty` promotion)
- Package-level `colorEnabled` bool and helper functions
- Constants: `ansiBold = "\033[1m"`, `ansiRed = "\033[31m"`, `ansiYellow = "\033[33m"`, `ansiBoldCyan = "\033[1;36m"`, `ansiReset = "\033[0m"`
- Helper `colorize(s, code string) string` — returns `code + s + ansiReset` if colorEnabled, else `s`
- Header line: all headers rendered in bold
- Priority cell: P0 = bold+red, P1 = yellow, P2+ = default
- `colorGroupHeader(s string) string` — wraps in bold cyan; used by both `ls.go` and `pipeline.go`

### Changes to `cmd/ls.go`

**Remove:** `printHeader()` and `printRow()` functions (moved to table.go as methods)

**`printGrouped()`:**
- Determine skip column from groupBy: `workflow`/`pipeline` → skip `"STAGE"`, `type` → skip `"TYPE"`, `priority` → skip `"P"`
- Exception: workflow grouping with group name `"Blocked"` → do NOT skip STAGE
- Create one `tableWriter` per group with appropriate skip set
- Group header: `fmt.Println(colorGroupHeader(fmt.Sprintf("=== %s ===", name)))`

**Flat mode / no grouping:**
- `newTableWriter()` with no skips → all columns shown

### Changes to other callers

`cmd/ready.go` (lines 46-48), `cmd/blocked.go` (lines 35-37), `cmd/closed.go` (lines 72-74):
```go
newTableWriter().Print(tickets)
```

`cmd/pipeline.go` (lines 69-73) — suppress STAGE and colorize its own group header format:
```go
fmt.Println(colorGroupHeader(fmt.Sprintf("=== %s (%d) ===", stage, len(group))))
newTableWriter("STAGE").Print(group)
```

**Out of scope:** `cmd/inbox.go` has its own two-line format (ticket + action detail) — not a tabular list, different output pattern.

### Files affected
1. **`cmd/table.go`** (new) — tableWriter, column defs, color helpers, colorEnabled init
2. **`cmd/ls.go`** — remove printHeader/printRow, update printGrouped to use tableWriter with skip logic
3. **`cmd/ready.go`** — switch to `newTableWriter().Print(tickets)`
4. **`cmd/blocked.go`** — switch to `newTableWriter().Print(tickets)`
5. **`cmd/pipeline.go`** — switch to `newTableWriter("STAGE").Print(group)`, colorize group header
6. **`cmd/closed.go`** — switch to `newTableWriter().Print(tickets)`

No dependency changes needed.

### Decisions
- **Decision:** Use `os.Stdout.Stat()` + `NO_COLOR` env check (matching `timeline.go` pattern) instead of `go-isatty` — avoids new direct dependency, consistent with existing codebase. (auto)
- **Decision:** Per-group column widths, not global — different groups may have different column sets due to suppression rules, so widths are computed per group. This means column offsets may vary between groups. (auto)
- **Decision:** Extend column suppression to `tk pipeline` — it groups by stage with its own loop, same logic applies naturally. (auto)
- **Decision:** `inbox.go` is out of scope — different output format (two-line per item with action detail). (auto)

## Test Results

## Test Results

### AC6: go test ./... — PASS
- `pkg/ticket` — PASS (0.282s)
- `internal/mcp` — PASS (0.750s)
- `internal/tui` — PASS (0.458s)
- `cmd/` — no test files (expected)

### AC1: Header rename — PASS
`go run . ls --flat | head -1` → `ID ... STAGE ... TITLE` (no STATUS)

### AC2: Dynamic column widths — PASS
All columns aligned. 30-char IDs like `change-description-front-fa8a` padded correctly.

### AC3: Column suppression — PASS
- `go run . ls` (default workflow grouping) — no STAGE column in groups
- `go run . ls --flat` — STAGE column present
- `go run . ls --group-by=type` — no TYPE column
- `go run . ls --group-by=priority` — no P column

### AC4: Color — PASS
- Piped output: 0 ANSI escape sequences (`go run . ls | cat | grep -c $'\\033'` → 0)
- TTY: visual inspection needed (code matches timeline.go pattern)

### AC5: No ID truncation — PASS
`change-description-front-fa8a` (30 chars) displayed in full.

## Review Log

**2026-03-12T06:57:02Z [agent:spec-builder]**
APPROVED — Spec validated after revision. All ACs now have concrete verification methods. AC2 tightened from "align cleanly" to explicit column offset assertion. AC3 clarifies workflow vs pipeline grouping and adds Blocked group exception. AC4 hardened from "e.g." to binding color mapping. AC5/AC2 overlap acknowledged.

**2026-03-12T07:07:55Z [agent:design-reviewer]**
APPROVED — Design validated after revision. All file paths and line numbers verified. TTY detection revised to match existing timeline.go pattern (os.Stdout.Stat + NO_COLOR). pipeline.go group header colorization explicitly addressed. inbox.go noted as out of scope. All 6 ACs have corresponding design steps. Per-group widths are intentional — column offsets may vary between groups.

**2026-03-12T07:10:33Z [human:steve]**
APPROVED — Design approved.

**2026-03-12T07:20:37Z [agent:code-reviewer]**
APPROVED — Approved. Clean refactor, correct padding-before-colorizing technique, ergonomic skip API. Non-blocking suggestions: magic string coupling for TITLE/P column dispatch, minor const alignment.

**2026-03-12T07:20:45Z [agent:impl-reviewer]**
APPROVED — Approved. All 6 ACs satisfied. All 6 design files modified as specified. No scope creep. Minor non-material deviation: ansiBoldRed combined constant instead of separate ansiRed — cleaner, same output.

**2026-03-12T08:10:36Z [human:steve]**
APPROVED — Verified. Global column widths, priority sort within groups, pipeline excludes done, colors match TUI for priority/type/stage.
