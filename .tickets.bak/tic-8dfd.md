---
id: tic-8dfd
status: closed
deps: []
links: []
created: 2026-02-22T22:02:10Z
type: task
priority: 3
assignee: Steve Macbeth
parent: tic-18e9
tags: [go-parity]
---
# Add interactive create mode




When tk create is run with no title and stdin is a TTY, bash version prompts for title, description, priority, type, and tags interactively.

## Design

Files: cmd/create.go
Detect TTY on stdin (os.Stdin.Stat, ModeCharDevice). When no title arg and TTY detected, prompt:
1. Title (required)
2. Description (optional, skip if -d provided)
3. Priority [0-4] default 2 (skip if -p provided)
4. Type [task/epic/bug/feature/chore] default task (skip if -t provided)
5. Tags comma-separated optional (skip if --tags provided)
Use bufio.Scanner for input.

## Acceptance Criteria

tk create with no args on TTY prompts for fields. Created ticket has correct values.

## Notes

**2026-02-23T04:42:23Z**

Already implemented via TUI create form (c key in tk ui). No separate CLI interactive mode needed.
