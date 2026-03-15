---
id: tk-ui-support-c849
stage: done
risk: low
deps: []
links: []
created: 2026-03-01T17:41:30Z
type: feature
priority: 1
skipped: [design]
---
# 'tk ui' support ctrl+j for newline in edit and create textboxes


Add ctrl+j support for inserting newlines in the TUI form textboxes (description and note fields). Currently all text fields are single-line only — enter submits the form. Multi-line fields should allow ctrl+j to insert a literal newline while enter continues to submit. ctrl+j sends LF (linefeed) which bubbletea reliably detects on all terminals without configuration.

## Test Results

All tests pass (19 wrapText tests including 6 new newline cases, plus form view test). Build succeeds.

## Notes

**2026-03-05T05:41:43Z**

Moved from tk-ui-support-b4ff in /Users/steve/code/forge
