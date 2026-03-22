---
id: tk-ui-edit-a0b1
stage: done
status: in_progress
deps: []
links: []
created: 2026-03-01T00:46:08Z
type: bug
priority: 1
---
# 'tk ui' on the edit panel editing a note flows off screen rather than wrapping










When editing a ticket in `tk ui`, the note field (and description) is a single-line text input that scrolls horizontally. Long text flows off the right edge of the screen instead of wrapping to the next line. This makes it difficult to compose or review longer notes.

## Root Cause

`form.go` renders all text fields as single-line inputs (line 283-308). The viewport calculation (`avail = m.width - 18`) creates a horizontal scrolling window. There is no line-wrapping or multi-line rendering for any text field.

## Fix

Convert the note and description fields to multi-line rendering with word wrapping. When the text exceeds `avail` width, wrap it onto subsequent lines. The cursor and viewport logic needs to account for wrapped lines.

Scope: `internal/tui/form.go` — the `view()` method's text field rendering (lines 283-308) and cursor/input handling.

## Acceptance Criteria

- When editing a ticket, the note field wraps long text to the next line instead of scrolling horizontally
- The description field also wraps long text
- Cursor navigation (left/right, home/end) works correctly across wrapped lines
- The form layout remains clean with wrapped fields taking additional vertical space
- Existing single-line fields (title, assignee) can remain single-line or wrap — consistent behavior preferred

## Test Results

All tests pass (go test ./...): pkg/ticket, internal/mcp, internal/tui. wrapText covers ASCII, multi-byte runes, exact-fit, remainder, single-char-width cases. FormView test verifies wrapping produces multiple lines without ellipsis truncation.

## Review Log

**2026-03-01T05:13:35Z [agent:code-review]**
APPROVED — Approved after rune-based fix. wrapText now operates on []rune, cursor rendering uses utf8.RuneCountInString for byte-to-rune conversion, and cursorCol is clamped to line length.

**2026-03-01T05:13:36Z [agent:impl-review]**
APPROVED — All 5 acceptance criteria satisfied. wrapText chunks text correctly, cursor decomposition maps flat offsets to visual line/column, all text fields get consistent wrapping.

**2026-03-01T05:40:50Z [human:steve]**
APPROVED — Verified visually — word wrapping works correctly in edit form.
