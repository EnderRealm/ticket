---
id: tk-ui-add-3f61
stage: implement
risk: low
deps: []
links: []
created: 2026-03-05T05:34:22Z
type: feature
priority: 2
branch: forge/tk-ui-add-3f61
run_id: orch-mmsrkl87-5yernu
---
# 'tk ui' add two additional spaces between id and title columns

## Acceptance Criteria

### Agent: Rundar (spec-builder) during spec @ 2026-03-08T17:46:55.366Z
### #####################################################################

Add two additional spaces (from 1 to 3) between the ID and TITLE columns in the `tk ui` dashboard list view, applied consistently to both the column header and each data row.

**Risk:** low

1. When the dashboard header is rendered, the system shall display 3 spaces between the padded ID column value and the TITLE label (e.g. `...ID   TITLE`), verified by a unit test in `internal/tui/dashboard_test.go` that calls `dashboardModel.view()` and asserts the header line contains `"ID   TITLE"` (three spaces).

2. When `renderRow` is called for a ticket with a known ID and title, the system shall produce output containing the padded ID followed by exactly 3 spaces and then the ticket title (e.g. `"test-abcd   My title"`), verified by a unit test in `internal/tui/dashboard_test.go` asserting `strings.Contains(row, t.ID+"   "+t.Title)`.

3. When a ticket has an active review indicator (`●`) prepended to the ID, the system shall still use exactly 3 spaces between the ID text and the title, verified by the same row unit test with `t.Review = ticket.ReviewPending`.

4. When the dashboard is rendered with zero tickets, the system shall not panic and shall still emit the column header with 3 spaces between ID and TITLE, verified by unit test calling `view()` on an empty `dashboardModel`.

## Design

### Agent: Helga (design-builder) during design @ 2026-03-08T17:55:53.527Z
### #####################################################################

**Approach:** Change two format strings in `dashboard.go` (header and row) from 1 space to 3 spaces between the ID and TITLE columns, and add a new `dashboard_test.go` with four unit tests covering each acceptance criterion.

### Architecture

This is a pure rendering change with no data-flow or state impact. Both the column header (in `view()`) and data rows (in `renderRow()`) are produced by `fmt.Sprintf` format strings in `internal/tui/dashboard.go`. The fix is to widen the separator literal in those two format strings from `" "` (1 space) to `"   "` (3 spaces). No new types, interfaces, or packages are introduced.

The new test file lives in the same `tui` package (package-internal, not `tui_test`), matching the existing pattern in `form_test.go`. Tests construct `dashboardModel` and `ticket.InboxItem` values directly and call `view()` / `renderRow()` to assert on the rendered strings with `strings.Contains`.

### Implementation Plan

1. **`internal/tui/dashboard.go`** – Update the header format string in `view()` (line 283):
   - Current: `fmt.Sprintf("%-3s %-6s %-10s %-*s %s", "P", "TYPE", "STAGE", idWidth, "ID", "TITLE")`
   - Change to: `fmt.Sprintf("%-3s %-6s %-10s %-*s   %s", "P", "TYPE", "STAGE", idWidth, "ID", "TITLE")`
   - This changes the single space between the `%-*s` (ID column) and `%s` (TITLE label) to three spaces.

2. **`internal/tui/dashboard.go`** – Update the row format string in `renderRow()` (line 350):
   - Current: `return fmt.Sprintf("%s  %s %s %s%s %s", pri, typ, stg, rev, idText, t.Title)`
   - Change to: `return fmt.Sprintf("%s  %s %s %s%s   %s", pri, typ, stg, rev, idText, t.Title)`
   - This changes the single space between `idText` and `t.Title` to three spaces. The `rev` variable (review indicator) occupies the position immediately before `idText` and is unaffected.

3. **`internal/tui/dashboard_test.go`** – Create new test file with package declaration `package tui` and imports `"strings"`, `"testing"`, `"github.com/EnderRealm/ticket/pkg/ticket"`:

   - **`TestDashboardHeaderThreeSpacesBetweenIDAndTitle`** (covers AC1):
     - Construct `m := dashboardModel{width: 80, height: 10}` (zero tickets, so `idWidth` stays at minimum of 2, meaning `"ID"` is not padded beyond its own length).
     - Call `output := m.view()`.
     - Assert `strings.Contains(output, "ID   TITLE")` (exactly 3 spaces). With `idWidth=2`, `fmt.Sprintf("%-*s   %s", 2, "ID", "TITLE")` produces `"ID   TITLE"`.

   - **`TestRenderRowThreeSpacesBetweenIDAndTitle`** (covers AC2):
     - Construct `tk := &ticket.Ticket{ID: "test-abcd", Title: "My title", Stage: ticket.StageTriage, Type: ticket.TypeFeature}`.
     - Construct `item := ticket.InboxItem{Ticket: tk, Action: ticket.ActionHumanInput}`.
     - Construct `m := dashboardModel{width: 80, height: 10}`.
     - Call `row := m.renderRow(item, false, len(tk.ID))`. Using `idWidth = len(tk.ID)` ensures no trailing-space padding, so `idText == tk.ID` with no extra spaces.
     - Assert `strings.Contains(row, tk.ID+"   "+tk.Title)`.

   - **`TestRenderRowReviewPendingThreeSpaces`** (covers AC3):
     - Same as above but set `tk.Review = ticket.ReviewPending` and `item.Action = ticket.ActionHumanReview`.
     - The review indicator `"● "` is rendered into `rev` and placed before `idText` in the format string; it does not appear between `idText` and `t.Title`.
     - Assert `strings.Contains(row, tk.ID+"   "+tk.Title)` — same assertion holds.

   - **`TestDashboardEmptyNoPanic`** (covers AC4):
     - Construct `m := dashboardModel{width: 80, height: 10}` (no tickets).
     - Call `output := m.view()` inside the test body (no `recover` wrapper needed — Go test runner catches panics as test failures, which is sufficient to verify no panic).
     - Assert `strings.Contains(output, "ID   TITLE")`.

### Key Decisions

- **Directly changing the format string literals** rather than introducing a named constant: The spacing is purely cosmetic and localized to two adjacent lines. A constant would add indirection without clarity benefit at this scale. Matches the project's existing inline-literal style throughout `dashboard.go`.

- **Using `idWidth = len(tk.ID)` in row tests** rather than passing an arbitrary larger width: Ensures `idText` equals exactly `tk.ID` with no trailing spaces, making the `strings.Contains(row, tk.ID+"   "+tk.Title)` assertion reliable without post-processing ANSI codes.

- **`package tui` (internal) not `package tui_test` (external)** for the test file: `renderRow` is an unexported method on `dashboardModel`. An external test package cannot call it. The existing `form_test.go` also uses `package tui` for the same reason.

- **No `newDashboardModel` call in tests**: `newDashboardModel` calls `buildItems()`, which filters tickets by stage and action kind. Constructing `dashboardModel{width: 80, height: 10}` directly and populating `InboxItem` manually avoids the filter logic and keeps tests focused on rendering only. This matches the spirit of the acceptance criteria which calls `renderRow` directly.

- **No ANSI-stripping in assertions**: In a non-TTY environment (Go test runner), lipgloss detects no terminal and emits unstyled output. The rendered strings for `idText` (non-selected row) and `t.Title` will be plain ASCII. This assumption is consistent with how `form_test.go` is written.

### #####################################################################

## Notes

**2026-03-16T05:51:01Z**

### Agent: Grimnar (surgical-coder) during implement @ 2026-03-16T05:51:01.119Z
### #####################################################################

## Summary

### What changed and why

Added two additional spaces (1 → 3) between the ID and TITLE columns in the `tk ui` dashboard, applied consistently to the header and data rows. Created a new test file with four unit tests covering all acceptance criteria.

### Files modified

1. **`internal/tui/dashboard.go`**
   - **Line 327** (header format string in `view()`): Changed `%-*s %s` → `%-*s   %s` (1 space → 3 spaces between ID and TITLE columns)
   - **Line 402** (row format string in `renderRow()`): Changed `%s %s` → `%s   %s` (1 space → 3 spaces between idText and t.Title)

2. **`internal/tui/dashboard_test.go`** (new file)
   - `TestDashboardHeaderThreeSpacesBetweenIDAndTitle` — AC1
   - `TestRenderRowThreeSpacesBetweenIDAndTitle` — AC2
   - `TestRenderRowReviewPendingThreeSpaces` — AC3
   - `TestDashboardEmptyNoPanic` — AC4

### Verification results

- **Build:** PASS
- **go vet:** PASS
- **Tests (internal/tui):** 4/4 PASS
- **Full suite:** 1 pre-existing failure in `pkg/ticket` (unrelated YAML parse error on `tk-ui-cursor-cc4b.md`)

### #####################################################################
