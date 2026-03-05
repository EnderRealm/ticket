---
id: tk-ui-add-539e
stage: verify
status: open
deps: []
links: []
created: 2026-03-01T02:32:06Z
type: feature
priority: 1
---
# 'tk ui' Add delete on main screen













Add ability to delete a ticket, with confirmationo

## Acceptance Criteria

1. When: user presses `d` on dashboard with a ticket selected, Then: inline confirmation prompt appears showing ticket title and "y/n" choice\n2. When: user presses `y` at confirmation prompt, Then: ticket file is deleted via store.Delete(), list refreshes, status message confirms deletion\n3. When: user presses `n` or `esc` at confirmation prompt, Then: prompt dismissed, no deletion\n4. When: user presses `d` with no tickets in list, Then: nothing happens\n5. Delete keybinding must appear in dashboard help bar

## Design

## Design\n\n### Dashboard Model\n- Add `confirmDelete bool` and `deleteTargetID string` fields\n- `inputActive()` returns true during confirmation to block App-level key routing\n\n### Key Flow\n1. `d` key (handled in tui.go App.Update) → sets confirmDelete state on dashboard\n2. During confirmDelete, dashboard.update handles `y` → emits `deleteTicketMsg`, anything else → cancels\n3. `deleteTicketMsg` dispatched to `handleDelete()` which calls `store.Delete(id)`\n4. Reload tickets and show status message\n\n### UI\n- Confirmation prompt rendered in help bar area (red, bold): \"Delete <id>? (y)es / (n)o\"\n- `(d)elete` added to default help bar"

## Test Results

All tests pass: go test ./... — internal/tui, internal/mcp, pkg/ticket all green.

## Review Log

**2026-03-05T05:49:43Z [agent:spec-reviewer]**
APPROVED — Simple feature, clear AC. Dashboard keybinding with inline confirmation, standard pattern.

**2026-03-05T05:51:05Z [agent:design-reviewer]**
APPROVED — Minimal design, follows existing patterns (move picker as precedent). Confirmation in help bar is clean.

**2026-03-05T05:51:14Z [agent:code-reviewer]**
APPROVED — Clean implementation. Confirmation state on dashboard model, delete msg dispatched at App level, help bar updated. Follows existing patterns.

**2026-03-05T05:51:18Z [agent:impl-reviewer]**
APPROVED — All AC covered. d key triggers confirmation, y deletes, any other key cancels, no-op on empty list, help bar updated.
