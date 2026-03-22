---
id: tk-ui-add-f586
stage: done
status: closed
deps: []
links: []
created: 2026-03-01T17:43:13Z
type: feature
priority: 3
---
# 'tk ui' add (m)ove to move a ticket to a new repo

























Add a (m)ove keybinding to the TUI that moves a ticket to a different repo. The move logic already exists in pkg/ticket/move.go and cmd/move.go — this feature wires it into the interactive TUI.

The user selects a ticket, presses 'm', enters a target repo path, and the ticket is moved using the existing MoveTicket function. Should support recursive move for epics.

## Design

### Changes

**detail.go:**
- Replace `inputMove` with `inputMovePicker` in `inputMode` enum
- Add picker state: `pickerItems []string`, `pickerCursor int`, `pickerTextActive bool`
- Add `discoverSiblingRepos(ticketsDir string) []string` — walks up to find git root, lists sibling dirs with `.git`, returns sorted paths
- Handle `inputMovePicker` in `updateInput`: j/k navigate, enter selects repo or activates text input for \"enter path...\", esc cancels
- Render picker as selectable list with \"move to repo:\" label
- Adjust `visibleRows` for picker height

**tui.go:**
- Update `m` keybinding in dashboard and detail views to use `inputMovePicker`
- Pass `ticketsDir` to detail model for repo discovery
- `moveTicketMsg` and `handleMove` unchanged

## Design

### Changes

**detail.go:**
- Add `inputMove` to `inputMode` enum
- Handle `inputMove` in `updateInput` → emit `moveTicketMsg`
- Add "move to repo:" label in input bar rendering
- Add `(m)ove` to help bar

**tui.go:**
- Add `moveTicketMsg{id, targetRepo string}` message type
- Add `m` keybinding in dashboard view → switch to detail + start move input
- Add `m` keybinding in detail view → start move input
- Add `handleMove(id, targetRepo)` handler that calls `ticket.MoveTicket(src, dst, id, false)`
- Handler creates dst FileStore from `filepath.Join(targetRepo, ".tickets")`, same as cmd/move.go

### Flow
1. User presses `m` (dashboard or detail)
2. Detail view activates with "move to repo:" input prompt
3. User types target repo path, presses Enter
4. `moveTicketMsg` emitted → `handleMove` called
5. `MoveTicket(src, dst, id, false)` executes
6. Status bar shows "Moved {old} → {new} in {repo}" or error
7. Tickets reload (moved ticket now closed)

### Decision: Non-recursive by default (auto)
Matches CLI behavior. Recursive move can be added later if needed.

## Acceptance Criteria

EARS acceptance criteria:

1. When the user presses 'm' on a selected ticket in the dashboard view, the TUI shall switch to the detail view with a move repo picker active.
2. When the user presses 'm' in the detail view, the TUI shall display a selectable list of sibling git repositories discovered by scanning directories adjacent to the current repo.
3. The picker shall include an \"enter path...\" option as the last item, which when selected activates a text input for manual path entry.
4. When the user selects a repo from the picker and presses enter, the TUI shall call MoveTicket with recursive=false and display a status message with the result.
5. When the move operation fails, the TUI shall display the error in the status bar.
6. When the picker is active, pressing Esc shall cancel the operation without side effects.
7. The help bar in detail and dashboard views shall include (m)ove in its list of available actions.
8. When no sibling repos are found, the picker shall show only the \"enter path...\" option.

## Test Results

All tests pass (go test ./...):\n- internal/mcp: ok (0.535s)\n- internal/tui: ok (0.285s)\n- pkg/ticket: ok (0.674s)\n\nBuild: clean (go build ./...)

## Review Log

**2026-03-05T05:03:25Z [agent:ghost]**
APPROVED — Spec is straightforward — wiring existing MoveTicket into TUI with inline input pattern already used by assignee/note.

**2026-03-05T05:03:44Z [agent:ghost]**
APPROVED — Design follows existing TUI patterns exactly. Two files, minimal changes.

**2026-03-05T05:06:15Z [agent:impl-reviewer]**
APPROVED — All 6 acceptance criteria satisfied. Design adherence confirmed across both files. No scope creep, no TODOs or placeholders.

**2026-03-05T05:06:16Z [agent:code-reviewer]**
APPROVED — Clean implementation following existing patterns. No security concerns. UX suggestion to auto-return to previous view after move was adopted.

**2026-03-05T05:31:35Z [human:steve]**
APPROVED — All 8 acceptance criteria verified. MoveTicket field-copy bug logged separately as moveticket-drops-stage-05c8.

## Notes

**2026-03-05T04:46:55Z**

Moved from tk-ui-add-1d0d in /Users/steve/code/forge

**2026-03-05T05:19:08Z**

Moved back to implement stage. AC revised to require sibling repo picker (AC 2, 3, 8) instead of raw text input. Current implementation only has text input — needs picker UI.

**2026-03-05T05:22:53Z**

Implemented repo picker for move. Replaces raw text input with inputMovePicker mode that scans sibling git repos via discoverSiblingRepos(). Includes "enter path..." fallback for manual entry. All 8 AC satisfied. Build clean, tests pass.

**2026-03-05T05:24:02Z**

## Test Results\n\n**2026-03-04:** 3 packages, all passed\n\n**Build:** pass\n**Test suite:** pass (pkg/ticket ok, internal/mcp ok, internal/tui ok)\n**Lint:** skipped (not configured)\n\n**Acceptance Criteria:**\n- [x] AC1 — 'm' in dashboard switches to detail with picker (tui.go:215-222)\n- [x] AC2 — 'm' in detail shows sibling repo picker via discoverSiblingRepos (tui.go:261-263, detail.go:84-105)\n- [x] AC3 — "enter path..." appended last, activates text input on select (detail.go:74-81, 226-229)\n- [x] AC4 — Selecting repo emits moveTicketMsg, handleMove calls MoveTicket(recursive=false) (detail.go:232-236, tui.go:582)\n- [x] AC5 — Move failure displays error in status bar (tui.go:583-585)\n- [x] AC6 — Esc cancels picker without side effects (detail.go:209-212)\n- [x] AC7 — Help bar shows (m)ove in detail and dashboard (detail.go:290, dashboard.go:293)\n- [x] AC8 — No sibling repos → only "enter path..." (detail.go:74-81)
