---
id: t-a5bc
status: closed
deps: []
links: []
created: 2026-02-22T00:57:32Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# Set up Go project structure

Initialize Go module, directory structure, and core dependencies.

## Design

Files: go.mod, main.go, cmd/root.go, pkg/ticket/, internal/tui/, internal/mcp/
Approach:
- go mod init github.com/wedow/ticket
- Dependencies: cobra, go-yaml v3, bubbletea, lipgloss
- main.go: minimal entry point calling cmd.Execute()
- cmd/root.go: root cobra command with --json global flag, TICKETS_DIR env var support
- Create empty package dirs with placeholder files

## Acceptance Criteria

go build produces a binary, tk help outputs usage


## Notes

**2026-02-22T01:16:05Z**

<!-- checkpoint: finalized -->
Go project scaffolded: go.mod, main.go, cmd/root.go, pkg/ticket/, internal/tui/, internal/mcp/. Binary builds, help outputs usage. Module path: github.com/EnderRealm/ticket (corrected from epic design notes).
