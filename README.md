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
go build -ldflags "-X github.com/EnderRealm/ticket/cmd.Version=2.1.0" -o tk .
```

Dev builds (`go build` with no ldflags) automatically show the git commit and dirty state via `runtime/debug.ReadBuildInfo`:

```
tk version
# dev (a1b2c3d, dirty)
```

## Getting Started

After installing, run setup to configure the central ticket store:

```bash
# First machine — create a new central store
tk setup --central-root ~/code/forge-data/tickets

# Register each project
cd ~/code/myproject
tk init --store central
```

On a second machine, point at the same repo:

```bash
# Clone the repo that holds your tickets
git clone git@github.com:YourOrg/forge-data.git ~/code/forge-data

# Point tk at it
tk setup --central-root ~/code/forge-data/tickets

# Register local projects
cd ~/code/myproject && tk init
```

## Configuration

Config lives in `~/.ticket/config.yaml` (created by `tk setup`):

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

`TICKETS_DIR` env var overrides all config-based resolution. `--repo` flag overrides everything.

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
  backlog [filters]          List tickets in the backlog
  ready [filters]            Tickets with all deps resolved and parent in_progress
  blocked [filters]          Tickets with unresolved deps
  done [--limit=N]           Recently done tickets

Creating & Editing:
  create [title] [options]   Create ticket (interactive if no title)
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)

Pipeline:
  advance <id> [--to stage]  Advance to next pipeline stage
  skip <id> --to <stage>     Skip ahead with --reason justification
  revert <id> --to <stage>   Revert to earlier stage with --reason
  review <id> --approve      Record review verdict (--approve or --reject)
  log <id>                   Show stage/review history
  pipeline [--stage X]       Show tickets grouped by pipeline stage
  inbox                      Show tickets needing human attention
  next                       Per-project next actions
  migrate [--dry-run]        Migrate legacy tickets to stage pipeline

Dependencies & Links:
  dep <id> <dep-id>          Add dependency
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree
  dep cycle                  Find cycles in open tickets
  link <id> <id> [id...]     Link tickets (symmetric)
  unlink <id> <target-id>    Remove link

Query:
  query [jq-filter]          Output tickets as JSONL (pipe to jq)

Analytics:
  stats                      Project health dashboard
  timeline [--weeks=N]       Tickets closed by week

Setup:
  setup [--central-root <path>]  First-run config (required before use)
  init [--store X] [--project X] [--yes] [--json]
                               Register a project with the central store
  sync                         Sync ticket changes to git

Interactive:
  ui                         Terminal UI (list + pipeline kanban view)
  serve                      MCP server for AI agent integration
  serve --central             Serve all projects from central ticket store

Other:
  workflow                   Ticket workflow guide
```

### Pipeline Stages

Tickets progress through type-dependent stage pipelines:

| Type | Pipeline |
|------|----------|
| feature | backlog → triage → spec → design → design-review → implement → code-review → test → verify → done |
| bug | backlog → triage → implement → code-review → test → verify → done |
| task | backlog → triage → done |
| chore | backlog → triage → spec → design → design-review → implement → code-review → test → verify → done |
| epic | backlog → triage → spec → design → done |

These are the default (normal risk) pipelines. Risk level controls pipeline shape: low-risk skips review stages, high/critical-risk bugs get the full feature pipeline (spec, design, design-review). Gate checks enforce preconditions at stage transitions (e.g., acceptance criteria before spec → design, review approval before design-review → implement).

### Filter Flags

```
--stage X         Filter by pipeline stage
-t, --type X      bug | feature | task | epic | chore
-P, --priority X  0 (critical) through 4 (backlog)
-a, --assignee X  Filter by assignee
-T, --tag X       Filter by tag
--parent X        Children of ticket X
--group-by X      Group by: workflow | pipeline | type | priority
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

Move all triage tickets to backlog:

```bash
tk query '.stage == "triage"' | jq -r '.id' | xargs -I{} tk revert {} --to backlog --reason "moving to backlog"
```

Partial ID matching: `tk show 5c4` matches `nw-5c46`.

### Git Sync

`tk serve` automatically commits and pushes ticket changes every 5 seconds. For manual sync:

```bash
tk sync
```

If a push conflict occurs, tk attempts `pull --rebase`. If rebase fails, sync is blocked and a `.tk-sync-blocked` marker is written. Resolve the conflict manually, then sync resumes on the next cycle.

### Multi-Project Serving

`tk serve --central` starts the MCP server with a `MultiStore` that serves all projects from the central ticket store. Ticket IDs are namespaced as `project/ticket-id`.

**Default project scoping:**
- When run from inside a project repo, tools default to that project's tickets (same behavior as single-project mode)
- When run outside any repo, tools return tickets from all projects
- The `project` parameter on `ticket_list`, `ticket_create`, `ticket_ready`, and `ticket_inbox` overrides the default

Other tools (`ticket_show`, `ticket_edit`, `ticket_advance`, etc.) accept namespaced IDs directly — pass `forge/my-ticket-1234` to operate on a specific project's ticket.

## Development

### Testing the MCP server locally

`.mcp.json` includes two dev server entries (both disabled by default) that point to the locally built `./tk` binary:

- **`tk-dev`** — single-project mode (`./tk serve`)
- **`tk-dev-central`** — multi-project mode (`./tk serve --central`)

To test MCP changes:

1. Build the binary:
   ```bash
   go build -o tk .
   ```

2. In Claude Code, open `/mcp` and:
   - Disable the global `plugin:forge:tk` server
   - Enable `tk-dev` (single-project) or `tk-dev-central` (multi-project)

3. When done, swap back: disable the dev server, re-enable `plugin:forge:tk`.

## Releasing

1. Update `CHANGELOG.md` — move `[Unreleased]` items under a versioned heading with today's date:

   ```markdown
   ## [2.1.0] - 2026-02-26
   ```

2. Commit and tag:

   ```bash
   git commit -am "release: v2.1.0"
   git tag v2.1.0
   git push && git push origin v2.1.0
   ```

3. GitHub Actions handles the rest:
   - **GoReleaser** builds darwin/linux binaries (amd64 + arm64)
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
