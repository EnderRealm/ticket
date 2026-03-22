---
id: mcp-ticket-edit-90f4
stage: done
status: in_progress
deps: []
links: []
created: 2026-03-07T00:03:01Z
type: bug
priority: 0
skipped: [implement, test, verify]
---
# MCP: ticket_edit might have a field limit

While trying to edit a ticket's design field the data got truncated. There might be an issue with the MCP server or the underlying ticket system. This is a test. This is a test dlkajsldkf jlaskdj flkajs dlkfj alskjd flkajs dlkfj alskdj flkajs dlkfj alskdjf lkasjd lfkjas ldkfj laksjd flkajs dlkfj aslkdj lkasjd flkj asdlkfj lkasj dflkja sdlkfj alksdj flkasj dflkj aslkdfj lkasjd flkjas dlkfj aslkdjf lkasjd flkj asldkfj laksjd flkjas dlkfj alskdj flkja sdlkfj alksjd flkajsdlkfj alskdj flkajs dflkjasd lkjalskdj flkaj sdlkfj alksdj flkaj sdlkfj alskdj flkajsd flkja sdlkfj laksjd flkja sdf

## Test Results

`go test ./...` passes. Two regression tests added: TestEditDesignWithMarkdownHeadings (## headings inside design content survive round-trip), TestEditDesignLongLine (70KB single-line content survives round-trip).

## Notes

**2026-03-10T04:48:26Z**

## Investigation

**Two bugs confirmed via reproduction tests:**

**Bug 1: `## ` headings inside content cause truncation**
- `parseSections` (mcp.go:143) treats any `## ` line as a section delimiter
- If design content contains `## Overview`, `## Architecture`, etc., everything after the first `## ` is dropped or misrouted
- `UpdateSection` (format.go:334) has the same problem — uses `"\n## "` to find section boundaries, splitting content mid-field
- Test: set design to `"## Overview\n\ntext\n\n## Architecture\n\nmore text"` → read back returns `nil`

**Bug 2: Lines >64KB cause silent data loss + panic**
- `splitFrontmatter` (format.go:142) uses `bufio.NewScanner` with default 64KB token limit
- Writing a 70KB single-line design field succeeds, but reading it back fails silently
- `registerEdit` at mcp.go:463 does `t, _ = store.Get(t.ID)` — error is swallowed
- `toJSON` receives nil ticket and panics (nil pointer dereference)

**Bug 1 is the likely culprit** for the reported truncation — design documents commonly use markdown headings.

**Root causes:**
1. Section parsing conflates document structure with content — no escaping or nesting awareness
2. Scanner buffer too small for large fields, and errors on re-read are silently discarded
