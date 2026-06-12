# ticket

A git-backed issue tracker for AI agents. Rooted in the Unix Philosophy, `tk` is inspired by Joe Armstrong's [Minimal Viable Program](https://joearms.github.io/published/2014-06-25-minimal-viable-program.html) with additional quality of life features for managing and querying against complex issue dependency graphs.

Tickets are markdown files with YAML frontmatter stored in a central repository. This allows AI agents to easily search them for relevant content without dumping ten thousand character JSONL lines into their context window.

## Install

### Homebrew (macOS / Linux)

```bash
brew install EnderRealm/tools/ticket
```

To upgrade after a new release:

```bash
brew update                        # fetch latest tap metadata
brew upgrade ticket
```

### From source

Requires Go 1.25+.

```bash
git clone https://github.com/EnderRealm/ticket.git
cd ticket
go build -o ~/.local/bin/tk .
```

## Build

Local development:

```bash
go build -o tk .
```

Release builds inject the version via ldflags:

```bash
go build -ldflags "-X github.com/EnderRealm/ticket/cmd.Version=7.6.0" -o tk .
```

Dev builds (`go build` with no ldflags) automatically show the git commit and dirty state via `runtime/debug.ReadBuildInfo`:

```
tk version
# dev (a1b2c3d, dirty)
```

## Getting Started

After installing, initialize from any project directory:

```bash
# First project — creates central store and registers the project
cd ~/code/myproject
tk init --central-root ~/code/forge-data/tickets
```

On a second machine, point at the same repo:

```bash
# Clone the repo that holds your tickets
git clone git@github.com:YourOrg/forge-data.git ~/code/forge-data

# Initialize and register projects
cd ~/code/myproject
tk init --central-root ~/code/forge-data/tickets
```

Subsequent projects on the same machine just need `tk init` (central root is remembered).

## Configuration

Config lives in `~/.ticket/config.yaml` (created by `tk init`):

```yaml
central_root: /Users/you/code/forge-data/tickets
git_email: tk@local
git_name: tk
default_store: central
sync_interval: 5s
projects:
    myproject:
        path: /Users/you/code/myproject
```

Shared project registry (store type, auto_link, etc.) is stored in `<central_root>/config.yaml` and synced via git alongside tickets.

`--repo` flag overrides project resolution for a single command.

### `spawn_command`

The TUI `w` keybinding (see below) launches a `/work <id>` session. The shell command it runs is configurable via the local `spawn_command` template, executed with `sh -c`. Two placeholders are substituted:

- `{dir}` — the ticket's project working directory (absolute path)
- `{id}` — the namespaced ticket ID (e.g. `myproject/tk-...`)

When unset, the default opens a new iTerm window (macOS), cds to the project, and starts Claude Code on the ticket:

```yaml
spawn_command: 'osascript -e ''tell application "iTerm"'' -e ''set w to (create window with default profile)'' -e ''tell current session of w to write text "cd '\''{dir}'\'' && claude \"/work {id}\""'' -e ''end tell'''
```

The default creates the window with a normal interactive shell and then types the command into it (via `write text`), so the window stays open and `claude` resolves on your `PATH`. It single-quotes `{dir}` so paths with spaces work; a project path containing a literal single quote can't be escaped inside the `osascript -e` wrapper — set a custom `spawn_command` for such paths.

Override to use a different terminal, e.g. tmux:

```yaml
spawn_command: 'tmux new-window -c {dir} "claude \"/work {id}\""'
```

## Agent Setup

Add this line to your `CLAUDE.md` or `AGENTS.md`:

```
This project uses a CLI ticket system for task management. Run `tk help` when you need to use it.
```

Claude Opus picks it up naturally from there. Other models may need additional guidance.

## Usage

Run `tk help` for the full command reference. Key commands:

```
Viewing:
  show <id> [--metadata]     Display ticket details
  ls|list [filters]          List tickets (default: workflow grouped)
  search <query>             Search tickets by relevance (best matches first)

Creating & Editing:
  create [title] [options]   Create ticket
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)

Dependencies & Links:
  dep <id> <dep-id>          Add dependency
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree
  link <id> <id> [id...]     Link tickets (symmetric)
  unlink <id> <target-id>    Remove link

Query:
  query [jq-filter]          Output tickets as JSONL (pipe to jq)

Setup:
  init [--project <name>] [--central-root <path>] [--yes]
                               Initialize tk and register a project
  sync                         Sync ticket changes to git
  status                       Show tk system status

Interactive:
  ui                         Terminal UI
  serve                      MCP server for AI agent integration

Journal:
  watch start [--interval=5s]  Start background git commit watcher
  watch stop                   Stop the background watcher
  watch status                 Show watcher status
  watch logs [-n 50]           Show watcher log output
  recompute [--project=NAME]   Rebuild commit journal from git history
```

### TUI Keybindings

The `tk ui` browser supports the usual navigation keys plus, in both the list and detail views:

| Key | Action |
|-----|--------|
| `y` | Yank (copy) the ticket ID to the clipboard |
| `w` | Spawn a `/work <id>` session in a new terminal (see `spawn_command`) |

In the list view, tab-specific status keys:

| Key | Action |
|-----|--------|
| `r` | Backlog tab: move ticket to ready |
| `b` | Inbox tab: move ticket to backlog |
| `x` | Inbox tab: mark ticket done |

### Statuses

Tickets use a simple status model:

| Status | Meaning |
|--------|---------|
| backlog | Waiting for grooming |
| ready | Available to work |
| open | Currently being worked on |
| done | Completed |
| closed | Not an issue, duplicate, etc. |

### Types

| Type | Purpose |
|------|---------|
| epic | Container for related features |
| feature | New functionality |
| bug | Defect fix |

### Filter Flags

```
--status X        Filter by status (backlog, ready, open, done, closed)
-t, --type X      bug | feature | epic
-P, --priority X  0 (critical) through 4 (backlog)
-T, --tag X       Filter by tag
--parent X        Children of ticket X
--group-by X      Group by: workflow | type | priority
--flat            Flat list (no grouping)
```

### Extra Fields

Tickets support arbitrary custom key/value metadata via `--set`:

```bash
tk create "Deploy config" --set env=production --set region=us-east
tk edit <id> --set env=staging        # update
tk edit <id> --set env=               # remove
```

Extra fields appear in `tk show` output, `tk query` JSONL (under `extra`), and MCP responses.

### Bulk Operations

Move all ready tickets to backlog:

```bash
tk query '.status == "ready"' | jq -r '.id' | xargs -I{} tk edit {} --status backlog
```

Partial ID matching: `tk show 5c4` matches `nw-5c46`.

### Git Sync

`tk serve` automatically commits and pushes ticket changes every 5 seconds. For manual sync:

```bash
tk sync
```

If a push conflict occurs, tk attempts `pull --rebase`. If rebase fails, sync is blocked and a `.tk-sync-blocked` marker is written. Resolve the conflict manually, then sync resumes on the next cycle.

### Multi-Project Serving

`tk serve` starts the MCP server with a `MultiStore` that serves all projects from the central ticket store. Ticket IDs are namespaced as `project/ticket-id`.

**Default project scoping:**
- When run from inside a project repo, tools default to that project's tickets
- When run outside any repo, tools return tickets from all projects
- The `project` parameter on `ticket_list`, `ticket_create`, `ticket_ready`, and `ticket_inbox` overrides the default

Other tools (`ticket_show`, `ticket_edit`, etc.) accept namespaced IDs directly — pass `forge/my-ticket-1234` to operate on a specific project's ticket.

## Development

### Testing the MCP server locally

`.mcp.json` includes a dev server entry (disabled by default) pointing to the locally built `./tk` binary:

- **`tk-dev`** — multi-project mode (`./tk serve`)

To test MCP changes:

1. Build the binary:
   ```bash
   go build -o tk .
   ```

2. In Claude Code, open `/mcp` and:
   - Disable the global `plugin:forge:tk` server
   - Enable `tk-dev`

3. When done, swap back: disable the dev server, re-enable `plugin:forge:tk`.

## Releasing

The git tag is the single source of truth for the version. There is no version constant in source: `cmd/root.go` declares `Version = "dev"`, and GoReleaser injects the tag's value via ldflags at build time. **Tagging is what releases** — pushing a `v*` tag triggers the build.

Pick the new version from the `[Unreleased]` changelog entries against the latest tag: new `Added`/`Changed` items → minor bump (`7.5.1` → `7.6.0`); `Fixed`-only → patch bump (`7.5.0` → `7.5.1`).

1. Run the tests — must be green:

   ```bash
   go test ./...
   ```

2. Update `CHANGELOG.md` — rename the `[Unreleased]` heading to a versioned heading with today's date:

   ```markdown
   ## [7.6.0] - 2026-06-08
   ```

3. Commit, then tag and push (commit and tag are pushed separately):

   ```bash
   git commit -am "release: v7.6.0"
   git tag v7.6.0
   git push
   git push origin v7.6.0
   ```

4. The `v*` tag push triggers GitHub Actions (`release --clean`); plain `master` pushes run CI only:
   - **GoReleaser** builds darwin/linux binaries (amd64 + arm64) and publishes a GitHub release with archives + checksums
   - **Homebrew** tap updated in `EnderRealm/homebrew-tools`

Required repository secrets: `GITHUB_TOKEN`, `TAP_GITHUB_TOKEN`.

### Monitoring & Debugging Releases

```bash
# Watch the release workflow
gh run list --limit 1
gh run watch <run-id> --exit-status

# If it fails, check logs
gh run view --log-failed

# If assets were partially uploaded (rerun fails with "already_exists"),
# delete the draft release and retry
gh release delete v2.1.0 --yes
gh run rerun --failed
```

`TAP_GITHUB_TOKEN` is a fine-grained PAT with Contents (read & write) permission on `EnderRealm/homebrew-tools`. If it expires, the Homebrew step will fail with a 401. Regenerate and update:

```bash
gh secret set TAP_GITHUB_TOKEN
```

## License

MIT
