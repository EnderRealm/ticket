# CLAUDE.md

## Task Management
'tk' is a CLI tool on PATH for task management. This project uses tickets to persistently manage all work items. Run 'tk help' for available commands and syntax. Tickets live in the central store (`<central_root>/tickets/<project>/`).

When adding/changing commands or flags, update:
1. The help text in `cmd/root.go`
2. The Usage section in `README.md`

## Architecture

Go binary. Four layers sharing one core library:

- `pkg/ticket/` — Core library (types, store, multistore, format, deps, filter, id, inbox, move)
- `cmd/` — CLI commands via cobra
- `internal/tui/` — Bubbletea TUI for interactive browse and edit
- `internal/mcp/` — MCP server for AI agent access via `tk serve`

`Store` is the interface; `FileStore` (single project) and `MultiStore` (central store, all projects) implement it. `tk serve` uses `MultiStore` with namespaced IDs (`project/ticket-id`).

Tickets are markdown files with YAML frontmatter. Core YAML fields: `id`, `status`, `deps`, `links`, `created`, `type`, `priority`, `parent`, `tags`.

Statuses: `backlog`, `ready`, `open`, `done`, `closed`.
Types: `epic`, `feature`, `bug`.

## Testing

```bash
# Run all tests
go test ./...

# Run specific package
go test ./pkg/ticket/
go test ./internal/mcp/
```

### MCP testing

MCP tools are tested in-process using the go-sdk's `NewInMemoryTransports`. The test harness in `internal/mcp/mcp_test.go` provides a `testServer(t)` helper that returns a connected `*mcp.ClientSession` backed by a temp directory. Use it to call any MCP tool without stdio:

```go
session := testServer(t)
result, err := session.CallTool(ctx, &mcp.CallToolParams{
    Name:      "ticket_create",
    Arguments: map[string]any{"title": "Test", "type": "feature"},
})
```

Do not replace the installed `tk` binary for testing — this machine runs MCP servers for other agents. Always use the in-process harness for unit tests.

### Live MCP testing

`.mcp.json` provides a disabled dev server pointing to the locally built `./tk` binary:

- **`tk-dev`** — multi-project mode (`./tk serve`)

To test MCP changes live:
1. `go build -o tk .`
2. In `/mcp`, disable `plugin:forge:tk`, enable `tk-dev`
3. Test via MCP tool calls
4. Swap back when done

## Changelog

When committing notable changes (new commands, flags, bug fixes, behavior changes), update CHANGELOG.md in the same commit:
- Create `## [Unreleased]` section at top if it doesn't exist
- Add bullet points under appropriate heading (Added, Fixed, Changed, Removed)
- Only code changes need logging; docs/workflow changes don't

## Releases & Packaging

Before tagging a release:
1. Ensure CHANGELOG.md has a section for the new version with release date
2. Update "Unreleased" to the version number and today's date
3. Commit the changelog update as part of the release

```bash
git commit -am "release: v2.2.0"
git tag v2.2.0
git push && git push origin v2.2.0
```

GitHub Actions automatically builds binaries via GoReleaser and updates the Homebrew formula in `EnderRealm/homebrew-tools`.
