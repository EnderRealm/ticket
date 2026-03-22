---
id: tic-a85c
status: closed
deps: []
links: []
created: 2026-02-22T22:02:27Z
type: task
priority: 3
assignee: Steve Macbeth
parent: tic-18e9
tags: [go-parity]
---
# Add NO_COLOR environment variable support




Bash version respects NO_COLOR env var to disable ANSI color output. Go version has no color in CLI output yet, but TUI uses lipgloss colors. Should be wired up if/when CLI gets colored output.

## Design

Files: cmd/root.go or pkg/ticket/color.go
Check os.Getenv("NO_COLOR") at startup. Pass through to any colored output. lipgloss respects this automatically via termenv, so TUI may already work. Verify and document.

## Acceptance Criteria

NO_COLOR=1 tk ls produces no ANSI escape sequences. TUI respects NO_COLOR.

## Notes

**2026-02-23T04:38:33Z**

Will not build. NO_COLOR already handled in timeline command. Not adding globally.
