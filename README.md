# ticket

A git-backed issue tracker for AI agents. Rooted in the Unix Philosophy, `tk` is inspired by Joe Armstrong's [Minimal Viable Program](https://joearms.github.io/published/2014-06-25-minimal-viable-program.html) with additional quality of life features for managing and querying against complex issue dependency graphs.

Tickets are markdown files with YAML frontmatter in `.tickets/`. This allows AI agents to easily search them for relevant content without dumping ten thousand character JSONL lines into their context window.

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

## Configuration

Set `TICKETS_DIR` to store tickets in a custom location (default: `.tickets`):

```bash
export TICKETS_DIR=".tasks"
tk create "my ticket"
```

Use `--repo` to operate on a different repo from anywhere:

```bash
tk ls --repo ~/code/other-project
tk show fix-auth --repo ~/code/other-project
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
  show <id>                  Display ticket details
  ls|list [filters]          List tickets (default: workflow grouped)
  ready [filters]            Tickets with all deps resolved and parent in_progress
  blocked [filters]          Tickets with unresolved deps
  closed [--limit=N]         Recently closed tickets

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

Interactive:
  ui                         Terminal UI (list + pipeline kanban view)
  serve                      MCP server for AI agent integration

Other:
  workflow                   Ticket workflow guide
```

### Pipeline Stages

Tickets progress through type-dependent stage pipelines:

| Type | Pipeline |
|------|----------|
| feature | triage → spec → design → implement → test → verify → done |
| bug | triage → implement → test → verify → done |
| task | triage → implement → test → verify → done |
| chore | triage → implement → done |
| epic | triage → spec → design → done |

Gate checks enforce preconditions at stage transitions (e.g., acceptance criteria before spec → design, review approval before implement → test). Gates scale by risk level: low (advisory), normal (standard), high/critical (strict).

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

Partial ID matching: `tk show 5c4` matches `nw-5c46`.

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
