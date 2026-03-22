---
id: tic-b6b1
status: closed
deps: []
links: []
created: 2026-02-22T22:01:39Z
type: task
priority: 0
assignee: Steve Macbeth
parent: tic-18e9
tags: [go-parity]
---
# Match help text to bash version





Go tk help output is 15 lines. Bash version is 85 lines with full command reference, query examples, filter docs, analytics section. Claude Code reads this to understand the tool.

## Design

Files: cmd/root.go
Replace rootCmd.Long with the exact bash help text (from cmd_help function). Cobra shows Long text for 'tk --help' and 'tk help'. Must include: Viewing section, Creating & Editing section, Dependencies & Links section, Query (JSON) section with examples, Analytics section, Other section, Filter flags for ls, Filter flags for ready/blocked/closed, Create & edit options.

## Acceptance Criteria

tk help output matches bash version exactly (modulo binary name). Includes all command groups, flag references, and query examples.

## Notes

**2026-02-22T22:07:27Z**

Help text now matches bash version. Custom help function suppresses Cobra's auto-generated command list. Added Interactive section for ui/serve (Go-only commands).
