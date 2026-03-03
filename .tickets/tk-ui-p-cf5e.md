---
id: tk-ui-p-cf5e
stage: verify
status: open
deps: []
links: []
created: 2026-03-01T00:43:24Z
type: bug
priority: 0
---
# 'tk ui' p for priority behaves weirdly



Pressing p changes the priority as it should. However, it also moves focus to the first ticket (should stay on the current ticket) and rorders the ticket window. Don't understand the ticket order.

## Review Log

**2026-03-02T09:52:12Z [agent:impl-reviewer]**
APPROVED — Fix verified: refreshTickets() preserves cursor; buildItems() sort matches Inbox() order.

**2026-03-02T10:00:17Z [agent:code-reviewer]**
APPROVED — Clean, targeted fix. ID-based cursor preservation is idiomatic; sort.SliceStable in buildItems decouples display from load order. Tests pass. Non-blocking suggestion: newDashboardModel/newPipelineModel are now dead code.

**2026-03-02T10:00:31Z [agent:code-reviewer]**
### FINDINGS — `buildItems()` inherited the `SortByStatusPriorityID` order (status-first), putting a P4 in-progress ticket above a P0 open ticket. Fixed by adding an explicit `sort.SliceStable` that sorts by priority ascending then age, matching the `Inbox()` order.

## Notes

**2026-03-02T09:56:52Z**

## Code Review

**Verdict:** APPROVED

### Findings

**Suggestions** (non-blocking):
- `internal/tui/dashboard.go:45` and `internal/tui/pipeline.go:80` — `newDashboardModel` and `newPipelineModel` are now dead code. Their only call sites in `tui.go` were replaced by `refreshTickets()`. Safe to delete, though not urgent.

**Positive:**
- `internal/tui/dashboard.go:61` — The ID-based cursor preservation pattern (save ID → rebuild → scan for ID → restore) is the right approach for Bubble Tea models. Robust against list reordering.
- `internal/tui/dashboard.go:122` — Adding an explicit `sort.SliceStable` in `buildItems()` decouples display order from load order (`SortByStatusPriorityID`). Correct and predictable.
- `internal/tui/pipeline.go:96` — The 2D cursor (stageCol + cardRow) preservation mirrors the dashboard fix cleanly and consistently.

### Security
No security concerns identified in this change.

### Summary
Clean, targeted fix for two distinct bugs with no unnecessary scope creep. The cursor-preservation approach is idiomatic and correct; the sort fix makes display order explicit rather than inherited from load order. All existing tests pass.
