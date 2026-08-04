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

The hierarchy is one level deep and enforced on write: `parent`, when set, must resolve to an epic in the same project, and an epic itself has no parent. Enforcement lives in `FileStore.Create`/`Update` (via `ResolveParent` in `pkg/ticket/parent.go`), the choke point `MultiStore` delegates to, so CLI, MCP and TUI writes are all covered — do not add a second check at a call site. `ResolveParent` also canonicalizes: a partial or namespaced parent is rewritten to the resolved epic's stored ID, because every reader matches by exact bare-ID equality.

Stores written before that rule still load and render; only writing one back is refused. `tk audit` reports the violations so a store can be cleaned first.

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

### How the version is determined

There is no version constant to bump in source. `cmd/root.go` declares `var Version = "dev"`, and the real value is injected at build time via ldflags from the git tag:

```
-X github.com/EnderRealm/ticket/v7/cmd.Version={{.Version}}
```

GoReleaser sets `{{.Version}}` from the tag being built (`v7.6.0` → `7.6.0`). So **the git tag is the single source of truth for the version** — tagging is what releases. A plain `go build` with no tag reports `dev (<short-sha>[, dirty])` via `debug.ReadBuildInfo`.

### Choosing the version (semver)

Look at the `[Unreleased]` section of CHANGELOG.md against the latest tag:
- New `Added`/`Changed` entries → minor bump (e.g. 7.5.1 → 7.6.0).
- Only `Fixed` → patch bump (e.g. 7.5.0 → 7.5.1).

### Cutting a release

1. Run `go test ./...` — must be green.
2. Rename the `[Unreleased]` heading in CHANGELOG.md to `[X.Y.Z] - YYYY-MM-DD` (today's date). Leave the bullets as-is.
3. Commit: `git commit -am "release: vX.Y.Z"`.
4. Tag and push (commit and tag are pushed separately):

```bash
git tag vX.Y.Z
git push
git push origin vX.Y.Z
```

### What the tag push triggers

The `v*` tag push fires `.github/workflows/` → GoReleaser (`release --clean`), which:
- builds `tk` for darwin/linux × amd64/arm64,
- publishes a GitHub release with archives + checksums (release notes use GitHub-native changelog),
- updates the Homebrew formula `ticket` in the `EnderRealm/homebrew-tools` tap.

Pushes to `master` without a tag run CI only — no release.
