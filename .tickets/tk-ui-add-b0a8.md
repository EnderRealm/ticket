---
id: tk-ui-add-b0a8
stage: design
status: open
review: rejected
deps: []
links: []
created: 2026-03-05T06:32:03Z
type: feature
priority: 2
---
# 'tk ui': Add header showing directory, tk version, and summary of ticket counts

## Acceptance Criteria

### Agent: Rundar (spec-builder) during spec @ 2026-03-06T07:25:10.852Z
### #####################################################################

Add a persistent 1-line header to the `tk ui` TUI that shows the repo directory name, tk version, and a summary of ticket counts. The header appears at the top of every view (dashboard, pipeline, detail, form) and is rendered by `App.View()` by prepending it before the current view's content.

**Risk:** low

1. When `tui.New()` is called, the system shall accept a `version string` second parameter and store it on the `App` struct for use in header rendering, verified by: Verify that `tui.New` in `internal/tui/tui.go` has signature `func New(ticketsDir string, version string) App` and that `cmd/ui.go` passes `version()` as the second argument.

2. When `App.View()` renders any view, the system shall output a 1-line header as the first line of output, followed by the current view's content on subsequent lines, verified by: Manual check — run `tk ui`, confirm first visible line of the TUI (line 1) is the header and the dashboard/pipeline content begins on line 2.

3. When the header is rendered, the system shall display the base name of the repo root directory (i.e., `filepath.Base(filepath.Dir(ticketsDir))`), verified by: Manual check — run `tk ui` from `/path/to/myrepo/.tickets`, confirm the header contains `myrepo`.

4. When the header is rendered, the system shall display the tk version string (the value passed to `tui.New`), verified by: Manual check — run `tk ui` and confirm the header contains the version string (e.g., `dev`, `dev (abc1234)`, or `v1.2.3`).

5. When the header is rendered, the system shall display the count of active tickets (those with `stage != ""` and `stage != "done"`), verified by: Manual check — run `tk ui` with a known set of tickets, confirm the active count matches the number of non-done, staged tickets in the `.tickets/` directory.

6. When the header is rendered, the system shall display per-stage counts in compact form for each stage that has at least one ticket, verified by: Manual check — run `tk ui`, confirm the header includes nonzero stage counts (e.g., `triage:2 implement:5 verify:1`), and that stages with zero tickets are omitted.

7. When a `tea.WindowSizeMsg` is received with height H, the system shall store `H` in `App.height` and pass `H-1` as the height to all four child models (`dashboardModel`, `pipelineModel`, `detailModel`, `formModel`) via their `setSize` calls, so all views have one fewer row to compensate for the header line, verified by: `go test ./internal/tui/ -run TestApp` (ensure existing tests still pass) plus manual check that the dashboard list does not overlap the help bar at the bottom.

8. When the tickets list is empty or not yet loaded (on startup before `ticketsLoadedMsg`), the header shall display `0 active` without panicking, verified by: Manual check — launch `tk ui` and confirm the header renders immediately without a crash on startup.

9. When the header content is wider than the terminal, the system shall not wrap the header onto a second line (the text may be truncated or clipped), so that layout is not disrupted, verified by: Manual check — resize the terminal to 40 columns, confirm the header occupies exactly 1 line.

## Scope

### In Scope
- `cmd/ui.go`: Update `tui.New(TicketsDir())` call to `tui.New(TicketsDir(), version())` — one line change.
- `internal/tui/tui.go`: Update `New()` signature to accept `version string`; add `version string` field to `App`; add `headerView()` helper that formats the header string; update `App.View()` to prepend `headerView() + "\n"`; update `WindowSizeMsg` handler to pass `a.height - 1` to all four `setSize` calls.
- `internal/tui/pipeline.go`, `dashboard.go`, `detail.go`, `form.go`: No changes required — height is set externally via `setSize`.

### Out of Scope
- Git branch display in the header: unrelated feature, separate ticket if desired.
- Making the header content or format configurable via flags/config.
- Showing inbox count (human-attention tickets) as a distinct metric — "per-stage counts" covers the same information more completely.
- Updating unit tests beyond ensuring existing tests still pass (TUI rendering is best verified manually; the height change does not affect the `form_test.go` wrap tests since those use fixed dimensions).
- Pipeline view (`viewPipeline`) navigation key `p` dispatched from `tui.go` — out of scope and unrelated.

## Open Questions
(none — sufficient information in codebase to write a complete spec)

### #####################################################################

## Design

### Agent: Helga (design-builder) during design @ 2026-03-06T07:30:49.725Z
### #####################################################################

**Approach:** Add `version` param to `tui.New()`, store `version` + computed `repoName` on `App`, implement `renderHeader()`, prepend it in `View()`, and reduce child height by 1 in all `setSize` / constructor calls.

### Architecture

The change is self-contained within the TUI layer. No new packages or types are needed.

- `App` grows two fields: `version string` (passed in) and `repoName string` (derived from `ticketsDir` at construction)
- `renderHeader()` is a new `App` method that computes the header from `a.repoName`, `a.version`, `a.tickets`, and `a.width`
- `View()` prepends `renderHeader() + "\n"` before the existing content string
- `WindowSizeMsg` in `Update()` passes `childH = max(0, a.height-1)` instead of `a.height` to all four `setSize` calls and to all child model constructors throughout `Update()`
- `cmd/ui.go` passes `version()` (already available in `cmd` package via `root.go`) as the second argument to `tui.New()`
- A new test file `internal/tui/tui_test.go` (package `tui`) holds the `TestApp*` tests

**Data flow for header:**
```
a.tickets ([]* ticket.Ticket)
  → count where Stage != "" && Stage != StageDone  → "N active"
  → count per Stage (only non-zero, in allStages order) → "triage:X implement:Y ..."
a.repoName   → e.g. "myrepo"
a.version    → e.g. "dev (abc1234)"
a.width      → clip header at terminal width via lipgloss MaxWidth
```

### Implementation Plan

1. **`internal/tui/tui.go`** — Add fields to `App` struct
   - Add `version  string` field after the existing `width int` field
   - Add `repoName string` field after `version`

2. **`internal/tui/tui.go`** — Update `New()` signature and body
   - Change signature to `func New(ticketsDir string, version string) App`
   - In the returned `App` literal, set `version: version`
   - Set `repoName: filepath.Base(filepath.Dir(ticketsDir))` (note: `filepath` is already imported)

3. **`internal/tui/tui.go`** — Fix `WindowSizeMsg` handler to reserve 1 row for header
   - Immediately after `a.width = msg.Width` / `a.height = msg.Height`, compute `childH := max(0, a.height-1)`
   - Replace all four `setSize(a.width, a.height)` calls with `setSize(a.width, childH)`:
     - `a.dashboard.setSize(a.width, childH)`
     - `a.detail.setSize(a.width, childH)`
     - `a.form.setSize(a.width, childH)`
     - `a.pipeline.setSize(a.width, childH)`

4. **`internal/tui/tui.go`** — Fix child model constructor calls to use `childH`
   - Define a helper `func (a App) childHeight() int { return max(0, a.height-1) }` — or inline `max(0, a.height-1)` at each site. Using a helper is cleaner given the number of sites.
   - Replace every `a.height` with `a.childHeight()` in the following constructor calls within `Update()`:
     - Line ~149: `newDetailModel(t, a.width, a.height)` inside `ticketsLoadedMsg` handler
     - Line ~204: `newFormModel(a.width, a.height)` (dashboard "c" key)
     - Line ~210: `newDetailModel(t, a.width, a.height)` (dashboard "enter"/"o" key)
     - Line ~217: `newEditFormModel(t, a.width, a.height)` (dashboard "e" key)
     - Line ~229: `newDetailModel(t, a.width, a.height)` (dashboard "m" key)
     - Line ~278: `newEditFormModel(a.detail.ticket, a.width, a.height)` (detail "e" key)
     - Line ~295: `newFormModel(a.width, a.height)` (pipeline "c" key)
     - Line ~300: `newDetailModel(t, a.width, a.height)` (pipeline "enter" key)
   - This ensures all newly-created children have the correct reduced height from the moment of creation

5. **`internal/tui/tui.go`** — Add `renderHeader()` method
   - Method signature: `func (a App) renderHeader() string`
   - Compute stage counts by iterating `a.tickets`; for each ticket where `t.Stage != "" && t.Stage != ticket.StageDone`, increment a `map[ticket.Stage]int` and a total counter
   - Build `parts []string`:
     1. `a.repoName`
     2. `"tk " + a.version`
     3. `fmt.Sprintf("%d active", total)`
     4. For each `s` in `allStages` (defined in `pipeline.go`, same package): if `counts[s] > 0`, append `fmt.Sprintf("%s:%d", s, counts[s])`
   - Join with `"  "` (two spaces): `header := strings.Join(parts, "  ")`
   - Apply style with truncation:
     ```go
     style := lipgloss.NewStyle().Bold(true)
     if a.width > 0 {
         style = style.MaxWidth(a.width)
     }
     return style.Render(header)
     ```
   - Note: `lipgloss` is already imported in the package (other files), so `tui.go` will need `"github.com/charmbracelet/lipgloss"` added to its import block; `ticket` package is already imported

6. **`internal/tui/tui.go`** — Update `View()` to prepend the header
   - After the `if a.err != nil` early return, compute `header := a.renderHeader()`
   - Keep all existing content-building logic unchanged
   - Change the final `return content` to `return header + "\n" + content`
   - The `a.status` replacement of the last line remains untouched (it operates on the child content before prepending the header, so the last line of `content` is still the child's help bar)

7. **`cmd/ui.go`** — Pass `version()` to `tui.New()`
   - Change `app := tui.New(TicketsDir())` to `app := tui.New(TicketsDir(), version())`
   - `version()` is already defined in `cmd/root.go` and is accessible within the `cmd` package; no import changes needed

8. **`internal/tui/tui_test.go`** (new file) — Add `TestApp` tests
   - File declaration: `package tui` (same package, needed to access unexported fields `dashboard.height`, `pipeline.height`, etc.)
   - Import: `"path/filepath"`, `"strings"`, `"testing"`, `tea "github.com/charmbracelet/bubbletea"`
   - **`TestApp_WindowSizePropagation`**:
     - Creates `app := New("/tmp/any/.tickets", "v1.0")`
     - Calls `model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})`
     - Casts `a := model.(App)`
     - Asserts `a.dashboard.height == 23`, `a.pipeline.height == 23`, `a.detail.height == 23`, `a.form.height == 23`
     - Also asserts `a.height == 24` (App stores full height, not reduced)
   - **`TestApp_RepoName`**:
     - Uses `t.TempDir()` to create a temp dir, appends `filepath.Join(base, "myrepo", ".tickets")`
     - Creates `app := New(ticketsDir, "testver")`
     - Asserts `app.repoName == "myrepo"`
   - **`TestApp_HeaderContainsVersionAndRepo`**:
     - Creates app with a known ticketsDir path and version
     - Sends `WindowSizeMsg{Width: 80, Height: 24}` to get a sized model
     - Calls `View()` on result
     - Splits on `"\n"` and asserts `lines[0]` contains the version string and repo name
     - Asserts `len(lines) >= 2` (header on line 0, content starting line 1)
   - **`TestApp_HeaderZeroTickets`**:
     - Creates app, sends `WindowSizeMsg`, calls `View()` without loading any tickets
     - Asserts no panic and `lines[0]` contains `"0 active"`

### Key Decisions

- **`childHeight()` helper method vs inline expression**: Using a private `childHeight() int` method avoids repeating `max(0, a.height-1)` across 8+ call sites and makes the intent explicit. Named helper matches existing codebase pattern of small helper methods on models.

- **`repoName` computed at construction time, not render time**: `filepath.Base(filepath.Dir(ticketsDir))` is stable for the lifetime of the app, so computing it once in `New()` is correct and avoids repeated work on every `View()` call.

- **Reuse `allStages` from `pipeline.go`**: It's in the same package and defines the canonical stage display order. No duplication needed.

- **Header style: `Bold(true)` only, no background**: Keeps the header visually distinct without the complexity of reverse-video or background colors that might not render well across terminals. The spec doesn't require specific styling.

- **`lipgloss.MaxWidth()` for truncation (AC9)**: This is the idiomatic lipgloss approach and handles ANSI escape sequences correctly. Guarded by `if a.width > 0` to avoid zero-width render issues on startup before the first `WindowSizeMsg`.

- **`App.height` stores full terminal height, `childH` is derived**: The App-level `height` field remains the raw terminal height (needed if any App-level logic ever needs it). Only the child models receive the reduced height.

- **Status bar replacement unchanged**: The existing `a.status` logic replaces `content`'s last line before prepending the header. This is correct: the last line of `content` is still the child's help bar, and the header line (prepended after) is never affected by status messages.

- **No separate `tui_test.go` already exists**: The only existing test file is `form_test.go`. Creating `tui_test.go` as package `tui` (not `tui_test`) is required to access unexported model fields for height assertions.

### #####################################################################

## Review Log

**2026-03-06T07:34:14Z [agent:design-reviewer]**
### ##################################################################### — the design calls it out but it warrants emphasis.

**2026-03-06T07:40:45Z [agent:design-reviewer]**
### ##################################################################### — **APPROVED / READY**.

**2026-03-06T07:46:30Z [agent:design-reviewer]**
### ##################################################################### — none of them affect correctness. **APPROVED.**

## Notes

**2026-03-06T07:34:14Z**

### Agent: Thrain (design-reviewer) during design @ 2026-03-06T07:34:14.520Z
### #####################################################################

**Gate Rejection (attempt 1/2)**

**Feedback:** Now I have everything I need to produce a thorough review. Let me compile the findings.

---

## Design Review

**Verdict:** READY

---

### Verified

- `internal/tui/tui.go` exists at the stated path; `App` struct has `width int` (line 34) and `height int` (line 35) fields exactly as described
- `New(ticketsDir string) App` currently takes one argument — signature change to two-arg is correctly identified
- `filepath` and `strings` are already imported in `tui.go` (lines 6, 8); no new imports needed for those packages
- `cmd/ui.go` calls `tui.New(TicketsDir())` at line 16 — matches what the design targets for Step 7
- `version()` function exists in `cmd/root.go` (line 126) and is package-level within `cmd`, accessible from `cmd/ui.go` ✓
- `allStages` is defined in `internal/tui/pipeline.go` (line 54), package `tui`, as `var allStages = []ticket.Stage{StageTriage, StageSpec, StageDesign, StageImplement, StageTest, StageVerify}` — **does NOT include `StageDone`**, which is exactly right for the header logic
- `ticket.StageDone` constant exists in `pkg/ticket/ticket.go` (line 44) ✓
- `lipgloss` dependency present in `go.mod` at `v1.1.0` ✓; `lipgloss` is currently **not** imported in `tui.go` but **is** imported in `pipeline.go`, `dashboard.go`, `detail.go`, `form.go` — design correctly identifies this gap
- All four `setSize` calls confirmed in the `WindowSizeMsg` handler (lines 129–132): `dashboard`, `detail`, `form`, `pipeline` ✓
- All 8 child-model constructor call sites confirmed in `Update()`:
  - Line 149: `newDetailModel(t, a.width, a.height)` — ticketsLoadedMsg refresh ✓
  - Line 204: `newFormModel(a.width, a.height)` — dashboard "c" ✓
  - Line 210: `newDetailModel(t, a.width, a.height)` — dashboard "enter"/"o" ✓
  - Line 217: `newEditFormModel(t, a.width, a.height)` — dashboard "e" ✓
  - Line 225: `newDetailModel(t, a.width, a.height)` — dashboard "m" ✓
  - Line 278: `newEditFormModel(a.detail.ticket, a.width, a.height)` — detail "e" ✓
  - Line 295: `newFormModel(a.width, a.height)` — pipeline "c" ✓
  - Line 301: `newDetailModel(t, a.width, a.height)` — pipeline "enter" ✓
- `dashboardModel`, `pipelineModel`, `detailModel`, `formModel` all expose `setSize(w, h int)` with pointer receivers ✓
- `newFormModel(w, h int)`, `newDetailModel(t *ticket.Ticket, w, h int)`, `newEditFormModel(t *ticket.Ticket, w, h int)` — constructor signatures match exactly ✓
- `a.tickets` is `[]*ticket.Ticket`; `range nil` is safe in Go — the zero-ticket case is panic-free ✓
- `form_test.go` uses `package tui` (not `tui_test`), confirming the pattern for accessing unexported fields; `tui_test.go` does not yet exist ✓
- `max()` is a Go built-in since 1.21; `go.mod` declares `go 1.25.6` ✓
- `View()` status-bar replacement operates on `content` before the proposed `header +"\n"+ content` concatenation — order is correct; header is unaffected by status messages ✓
- `TicketsDir()` fallback returns `".tickets"` (relative path) — `filepath.Base(filepath.Dir(".tickets"))` = `"."`, a harmless edge case ✓

---

### Issues

- **[WARNING]** `tui.go` needs `"github.com/charmbracelet/lipgloss"` added to its import block
  - Detail: `tui.go` currently imports `fmt`, `path/filepath`, `strings`, `time`, `bubbletea`, and `ticket` — no `lipgloss`. The design correctly calls this out in Step 5, but it's an easy omission during implementation since `lipgloss` is freely used in sibling files.
  - Suggestion: The implementation plan should add an explicit sub-bullet to Step 5 (or Step 1): "Add `\"github.com/charmbracelet/lipgloss\"` to `tui.go`'s import block."

- **[WARNING]** `App.View()` early-return path (`a.err != nil`) does not prepend the header
  - Detail: The design says "After the `if a.err != nil` early return, compute `header := a.renderHeader()`". The error path returns a plain `fmt.Sprintf("Error: %v\n", a.err)` with no header. This is the intended behaviour (broken state = no header), but it means the terminal may reflow if the error fires after the header was previously shown.
  - Suggestion: Acceptable as-is — error state → quit is the current pattern (line 158: `return a, tea.Quit`). No change needed, just be aware.

- **[WARNING]** `TestApp_WindowSizePropagation` assertion on `a.form.height` and `a.detail.height` may require clarification in the test comment
  - Detail: `formModel.setSize` and `detailModel.setSize` both use pointer receivers. Within `Update()` (value receiver on `App`), Go auto-takes the address of the embedded field on the local copy, which is correct. However, the test relies on the returned `tea.Model` being type-assertable to `App` — if bubbletea ever wraps the model, the assertion could fail. Currently bubbletea returns the model as-is, so this is fine.
  - Suggestion: Add a comment in the test noting the type assertion assumption.

---

### Acceptance Criteria Coverage

- [x] **AC1** — `tui.New` two-arg signature + `cmd/ui.go` passes `version()` — covered by Steps 2 and 7
- [x] **AC2** — 1-line header as first line of `View()` output — covered by Step 6
- [x] **AC3** — Repo base name displayed in header — covered by Step 2 (`filepath.Base(filepath.Dir(ticketsDir))`)
- [x] **AC4** — tk version string in header — covered by Steps 2 and 5
- [x] **AC5** — Active ticket count (`stage != "" && stage != "done"`) — covered by Step 5
- [x] **AC6** — Per-stage compact counts, zero-count stages omitted — covered by Step 5 (iterates `allStages`, skips zero counts)
- [x] **AC7** — `WindowSizeMsg` stores full height in `App.height`, passes `H-1` to all four child models — covered by Steps 3 and 4
- [x] **AC8** — No panic, shows `"0 active"` before tickets load — covered by Step 5 (range over nil `a.tickets` is safe) and Step 8 (`TestApp_HeaderZeroTickets`)
- [x] **AC9** — Header truncated to terminal width, no wrapping — covered by Step 5 (`lipgloss.MaxWidth(a.width)` guarded by `a.width > 0`)

---

### Risks

- **`allStages` reuse**: If the pipeline definition in `pipeline.go` ever adds or reorders stages, the header will automatically reflect that order. This is the desired behavior (single source of truth), but callers should be aware that `allStages` is an internal `tui`-package variable — if it were ever moved or renamed, the header code would need updating.
- **`lipgloss.MaxWidth` semantics**: In `lipgloss v1.1.0`, `MaxWidth` truncates ANSI-safe at the specified width. Since `renderHeader()` calls `Bold(true)` before `MaxWidth`, the bold ANSI codes are correctly accounted for in the width calculation. No issue anticipated.
- **Relative path `TicketsDir()` fallback**: When running `tk ui` outside any repo (no `.tickets/` directory found), `TicketsDir()` returns `".tickets"`, which makes `repoName = "."`. The header would show `"."` instead of a meaningful name. This is a pre-existing UX limitation, not introduced by this change.

### Gaps

- None identified. The design is complete for the stated scope.

---

**Summary:** The design is sound and well-grounded in the actual codebase. All file paths exist, all APIs match their actual signatures, `allStages` is accessible from `tui.go` (same package), `version()` is accessible from `cmd/ui.go` (same `cmd` package), and all 9 acceptance criteria are explicitly covered by the implementation steps. The only actionable note is to ensure the `lipgloss` import is not forgotten when implementing Step 5 — the design calls it out but it warrants emphasis.

Use this feedback to improve the next attempt.

### #####################################################################

**2026-03-06T07:40:45Z**

### Agent: Thrain (design-reviewer) during design @ 2026-03-06T07:40:45.225Z
### #####################################################################

**Gate Rejection (attempt 2/2)**

**Feedback:** Now I have everything I need. Let me compile the full review.

---

## Design Review

**Verdict:** READY

### Verified

- **`internal/tui/tui.go` exists** at the stated path; current `New()` signature is `func New(ticketsDir string) App` — change to add `version string` param is well-scoped.
- **`cmd/ui.go` exists** with the exact call `app := tui.New(TicketsDir())` that needs updating (line 16).
- **`version()` function** is package-level unexported in `cmd/root.go` (lines 126–152), accessible from `cmd/ui.go` in the same package. No import changes needed.
- **`TicketsDir()` function** exists in `cmd/root.go` (line 195), confirming the call site pattern.
- **`allStages` variable** is defined in `internal/tui/pipeline.go` (line 54), same `tui` package. Includes Triage→Verify (6 stages), excludes `StageDone`. Correct for header display.
- **`ticket.StageDone`** confirmed in `pkg/ticket/ticket.go` (line 44) as `Stage = "done"`.
- **`filepath` already imported** in `tui.go` (line 6). ✓
- **`strings` already imported** in `tui.go` (line 7). ✓
- **`fmt` already imported** in `tui.go` (line 5). ✓
- **`ticket` package already imported** in `tui.go` (line 12 — `github.com/EnderRealm/ticket/pkg/ticket`). ✓
- **`lipgloss` NOT currently imported in `tui.go`** — design correctly flags this as needing addition; it's present in all other tui files (pipeline.go, dashboard.go, detail.go, form.go).
- **`lipgloss v1.1.0`** is in `go.mod`/`go.sum`; `.MaxWidth(int)` has been stable API since before v1.0.
- **All 4 `setSize` calls** in `WindowSizeMsg` handler correctly identified (lines 129–132).
- **All 8 constructor call sites** with `a.height` correctly identified and cross-checked against actual line numbers: 149, 204, 210, 217, 224, 278, 295, 301. Zero omissions.
- **No additional `a.height` usages** exist in `tui.go` beyond the 12 covered by steps 3 and 4.
- **`setSize` pointer receivers** (`*dashboardModel`, `*detailModel`, `*formModel`, `*pipelineModel`) will work correctly on addressable fields of the local `a App` value.
- **`repoName` computation** `filepath.Base(filepath.Dir(ticketsDir))` correctly extracts the repo name from e.g. `/path/to/myrepo/.tickets` → `"myrepo"`.
- **Status-bar replacement** in `View()` operates on `content` (child view) before prepending header; the header is never clobbered by status messages. ✓
- **`tui_test.go` does not yet exist** — safe to create. `form_test.go` is `package tui` (same-package), confirming the test pattern for accessing unexported fields. ✓
- **`dashboard.view()` / `pipeline.view()` guard** against zero width/height (`if m.width == 0 || m.height == 0 { return "" }`) will not be triggered after `WindowSizeMsg{Width:80, Height:24}` is processed.
- **Nil `a.tickets` slice**: iterating over it in `renderHeader()` is safe in Go (no-op loop); `total` stays 0 and `"0 active"` renders without panic (AC8). ✓
- **All child view functions end without a trailing `"\n"`** (dashboard, pipeline, detail, form) — confirms the existing status-line replacement logic finds the correct last element after `strings.Split`. ✓

### Issues

No blockers or errors found.

### Acceptance Criteria Coverage

- [x] **AC1** — `tui.New()` accepts `version string` and `cmd/ui.go` passes `version()` — covered by Steps 2 and 7.
- [x] **AC2** — Header as first line of `View()` output — covered by Step 6.
- [x] **AC3** — Repo directory base name in header — covered by Step 2 (`repoName: filepath.Base(filepath.Dir(ticketsDir))`), rendered in Step 5.
- [x] **AC4** — tk version string in header — covered by Steps 1, 2, 5.
- [x] **AC5** — Active ticket count (`stage != "" && stage != "done"`) — covered by Step 5 (`renderHeader()`).
- [x] **AC6** — Per-stage counts (non-zero only, canonical order) — covered by Step 5 (iterating `allStages`).
- [x] **AC7** — `WindowSizeMsg` stores full height in `App.height`, passes `H-1` to child models — covered by Steps 3 and 4; verified by `TestApp_WindowSizePropagation`.
- [x] **AC8** — `0 active` on startup without panic — covered by nil-safe slice iteration in `renderHeader()`; verified by `TestApp_HeaderZeroTickets`.
- [x] **AC9** — No line-wrap on narrow terminals — covered by `lipgloss.MaxWidth(a.width)` in `renderHeader()`; guarded by `if a.width > 0`.

### Risks

- **`repoName` degrades to `"."` in fallback mode**: When `TicketsDir()` returns the bare fallback `".tickets"` (no `.tickets/` directory found), `filepath.Base(filepath.Dir(".tickets"))` = `"."`. The header would show `"."` instead of a meaningful repo name. This is a corner-case edge condition (user running `tk ui` with no `.tickets` dir), low impact, and not worth blocking the design over — but implementers should be aware.

- **Lipgloss Bold + MaxWidth in non-TTY test environments**: `lipgloss.NewStyle().Bold(true)` may emit ANSI escape sequences even in test environments depending on how lipgloss detects the terminal profile. `strings.Contains(lines[0], versionString)` is still correct because the literal version text is preserved between any ANSI codes. Tests should pass reliably.

- **Per-stage counts don't map legacy tickets** (those with `Stage == ""`): The `renderHeader()` design only counts tickets by their direct `t.Stage` value. The pipeline view maps legacy tickets (`Stage == ""`) to stages via `ticket.StatusToStage`, but the header doesn't do this. The "active" count (AC5) correctly excludes them (`t.Stage != ""`), but a user with old tickets may see an "active count" that doesn't match the pipeline column sums. This is a design decision consistent with AC5's stated criterion; acceptable as-is.

### Gaps

None identified. The design is complete, internally consistent, and all implementation sites in the codebase are correctly identified.

---

**Summary:** All file paths verified, all API signatures confirmed, all constructor and `setSize` call sites accounted for (12 total), import requirements accurate, test strategy is sound and follows existing `package tui` conventions. The design is approved as written — **APPROVED / READY**.

Use this feedback to improve the next attempt.

### #####################################################################

**2026-03-06T07:46:30Z**

### Agent: Orchestrator (orchestrator) during design @ 2026-03-06T07:46:30.067Z
### #####################################################################

Gate checks failed after max retries. Run stopped.

### #####################################################################
