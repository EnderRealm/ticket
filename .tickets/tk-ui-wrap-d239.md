---
id: tk-ui-wrap-d239
stage: done
status: in_progress
risk: low
deps: []
links: []
created: 2026-03-10T05:11:13Z
type: bug
priority: 1
---
# 'tk ui' wrap description field on long line length like other fields

The detail view in `tk ui` renders body text, notes, and review log entries without word-wrapping. Long lines extend beyond the terminal width instead of wrapping like the form view does. The `wrapText()` function already exists in the same package but is only used in form.go.

## Review Log

**2026-03-12T03:08:01Z [agent:ghost]**
APPROVED — Minimal fix reusing existing wrapText() for body, review log, and notes in detail view. Builds clean, all tests pass.
