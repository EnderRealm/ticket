# Changelog

## [7.5.0] - 2026-06-07

### Added
- `tk search <query>` CLI command and `ticket_search` MCP tool: rank a project's tickets by token-based relevance across title, body, and notes (best matches first) to find similar or duplicate tickets before creating new ones. Each result shows a context snippet of where the query matched — the CLI prints a dimmed excerpt with matched terms bolded; the MCP tool returns `match_field` and `snippet` per match.
- TUI: `w` keybinding (list and detail views) spawns a new terminal session running `claude "/work <id>"` in the ticket's project directory. The launch command is a configurable `spawn_command` template (`{dir}`/`{id}` placeholders, run via `sh -c`); the default opens a new iTerm window on macOS.

## [7.4.0] - 2026-05-31

### Added
- Persisted `updated` and `completed` timestamps on tickets, stamped on every create and update at the store's single write choke point (CLI, MCP, TUI).
- TUI dashboard time columns between STATUS and TITLE, varying per tab: CREATED and MODIFIED everywhere, plus AGE (inbox/backlog), COMPLETED and DURATION (done), and an adaptive AGE/DURATION column (all).
- TUI interactive sorting: `s` cycles the sort column, `S` toggles direction, with the active column's header underlined and arrowed. Applies to the ticket tabs and the epics tab (epics tab defaults to AGE descending and sorts the top-level epic rows only).

### Changed
- TUI dashboard rows and headers are now column-driven so they stay aligned.
- TUI: the create/edit form Type, Priority, and Status selectors now use neutral uniform colors with a focus-aware selection highlight (dim unselected, bold-white selected, inverse accent chip when the row is focused) instead of per-domain colors.

### Fixed
- Core: `Serialize` no longer writes the `updated` field when it is the zero value, so legacy/unstamped tickets stop showing `updated: 0001-01-01T00:00:00Z` (matches the existing handling of `completed`).
- CLI: `tk show` (default) and `tk create` now render the `created`, `updated`, and `completed` frontmatter timestamps in local wall-clock time instead of UTC. Storage, `tk show --metadata`, and machine-facing JSON output remain UTC.
- TUI: search mode is now navigable. While the filter box is active, arrow keys and the mouse wheel move the selection through the filtered results, and `enter` commits the filter — closing the search box but keeping the list narrowed so row commands (edit/move/delete/priority/yank) operate on the filtered set. Press `enter`/`o` again to open the highlighted ticket. Typing still refines the filter, backspace edits, and esc clears.
- TUI: clearing the search with esc now keeps the cursor on the selected row instead of resetting to the top of the list.
- TUI: while the search box is active, the footer help shows only the keys that work in search mode (navigate, apply, clear) instead of the full command list.
- TUI: sorting by the TITLE column sorted by priority instead of title. The column had no comparator, so it fell through to the priority fallback; it now sorts alphabetically (case-insensitive).
- TUI: the active sort arrow on the MODIFIED and DURATION headers touched the next column; both widened so the arrow always has a gap.

## [7.3.0] - 2026-05-30

### Added
- TUI: header now shows an info segment flush right of the tab bar with the launch directory (HOME abbreviated to `~`), the tk version, and ticket counts by status (open/ready/backlog/done). The segment right-aligns when it fits and is dropped on terminals too narrow to hold it.
- TUI: `y` (yank) keybinding copies the selected/open ticket's ID to the system clipboard from both the list and detail views, with a transient "Copied ID" confirmation. The ID is the token you paste into `/work` and other tk commands.

### Fixed
- TUI: Detail view now renders the created date and note timestamps in local time. They are stored as UTC, but `tk ui` was formatting the raw UTC value, so the displayed times were off by the machine's UTC offset. Storage and machine-facing JSON output remain UTC.

## [7.2.1] - 2026-04-27

### Fixed
- Sync: refuse to commit when the working tree has unmerged paths or staged blobs contain git conflict markers. `pull --rebase --autostash` exits 0 even on stash-pop conflicts, so 7.2 could commit and push a `config.yaml` with `<<<<<<< / ======= / >>>>>>>` markers when two machines registered a project within seconds of each other. The corrupted shared config then made every project fall back off the central store and `tk list` report them as empty.
- Config: `project.Load()` no longer silently swallows shared-config parse errors. A missing shared config remains optional, but a corrupt one now returns the parse error so callers see "shared config corrupted at line N" instead of an empty project list.

## [7.2.0] - 2026-04-25

### Fixed
- Sync: every cycle now fetches origin and rebases when behind, independent of whether there are local commits to push. Previously a machine with no outgoing changes never picked up incoming ones, letting machines diverge silently. Rebase uses `--autostash` (safe in a tk-only central store) and writes `.tk-sync-blocked` on failure.
- Core: `AddDep`, `RemoveDep`, `AddLink`, `RemoveLink` now compare ticket IDs tolerantly of `project/` namespace prefixes. Previously `ticket_dep remove` silently no-op'd when the stored dep was in one form (e.g. `foo-abcd`) and the MCP tool resolved it to the other form (`ticket/foo-abcd`).
- TUI: A file-watcher refresh no longer resets an in-flight detail overlay (move picker, note entry, path input). Refreshes skip the rebuild while input is active; the overlay updates on the next load after input closes.
- MCP: `ticket_show` now trims notes to the 20 newest by default and exposes `notes_limit` (0 = all), `notes_offset`, and `metadata_only` arguments. Response includes `notes_total` and `notes_shown` so callers know whether there is more history to fetch.

### Added
- Core: Epic status validation — saving a type=epic ticket with status=done is rejected when any child is still non-terminal. Error names the offending children and suggests remediation.
- Core: Upward status propagation — child → ready bumps a backlog epic to ready; child → open bumps a backlog/ready epic to open; marking the last non-terminal child done auto-marks the parent epic done. Cascades up nested epic chains.
- MCP: Tool input types are now flexible — LLMs that send numeric arguments (priority, offset, limit) as JSON strings have them coerced to integers instead of being rejected at schema validation. Pattern generalises via FlexInt/FlexBool helpers for future fields.

### Changed
- TUI: Create/edit form Enter key now inserts a newline on multiline fields (Description, Note) instead of submitting. ctrl+s remains the explicit save; Enter on single-line fields and Type/Priority/Status toggles is unchanged.

### Fixed
- TUI: Create/edit form selected chip (type / priority / status) now renders as bold black text on the chip's own domain color instead of layering the domain color over yellow, which produced low-contrast combinations like red-on-yellow

## [7.1.0] - 2026-04-19

### Added
- test-suite.sh: Summary of failed tests printed at the end of a failing run

### Changed
- TUI: Inbox, done, and all tabs now exclude epic-type tickets (epics live in the epics tab)
- TUI: Backlog tab shows epics as rollup rows with a child count and hides epic children; selecting an epic jumps to the epics tab focused on that epic
- test-suite.sh: Rewritten against v7 commands, runs in an isolated temp central store so local tickets are untouched

## [7.0.0] - 2026-04-11

### Changed
- Core: Replaced 10-stage pipeline system with flat status model (backlog, ready, open, done, closed)
- Core: Simplified ticket types to epic, feature, bug (removed task, chore)
- Core: Removed pipeline, gates, workflow, risk levels, and formal review system
- Core: Central store is now the only storage mode (removed local .tickets/ support)
- CLI: Removed commands: advance, skip, revert, pipeline, migrate, workflow, review
- CLI: `tk serve` always uses MultiStore (removed --central flag)
- CLI: `tk init` always uses central store (removed --store flag)
- CLI: Filter flag `--stage` replaced with `--status`
- CLI: Default ticket type changed from task to feature
- MCP: Removed tools: ticket_advance, ticket_skip, ticket_revert, ticket_migrate, ticket_pipelines, ticket_review, ticket_workflow
- MCP: JSON output uses `status` field instead of `stage`; removed `review`, `risk`, `skipped` fields
- TUI: Simplified tabs to Inbox, Backlog, Epics, Done, All (removed Triage tab)
- TUI: Removed review overlay
- Journal: Auto-close uses direct status update instead of pipeline Skip()
- CLI: `tk init` now handles first-run setup (folded in from removed `tk setup`)

### Removed
- CLI commands: backlog, ready, blocked, done, log, inbox, next, stats, timeline, setup, review, dep cycle
- `pkg/ticket/pipeline.go`, `pipelines.json`, `gates.go`, `config.go`, `workflow.go`, `migrate.go`
- Ticket fields: Stage, Review, Risk, Skipped, Reviews, Assignee, Conversations
- ReviewState, RiskLevel, ReviewRecord types
- Stage type and all stage constants
- Create/edit flags: `--design`, `--acceptance`, `--assignee`

## [6.0.1] - 2026-04-06

### Fixed
- TUI: Search/filter mode no longer triggers global shortcuts (quit, create, open) on overlapping key presses
- TUI: Epics tab hides done epics and sorts by stage (pipeline order); header count excludes done

## [6.0.0] - 2026-04-05

### Added
- TUI: Two-tab layout (Epics / Tickets) replacing old dashboard/pipeline views
- TUI: Header with project name and ticket count stats
- TUI: Command bar (Ctrl+K) with search and /command stub
- TUI: Overlay infrastructure for detail, form, and review views
- TUI: Epics tab placeholder with progress bars

### Fixed
- `tk init --store central` wrote project directories to central store root instead of `tickets/` subdirectory

### Changed
- TUI: Detail, form, and review views now open as overlays instead of full view switches
- TUI: Pipeline view removed (replaced by Epics tab)

## [5.6.0] - 2026-03-28

### Added
- Journal: `pkg/journal` package for commit-to-ticket linking via JSONL append-only journal
- CLI: `tk watch start|stop|status|logs` — background daemon that polls git log and links commits to tickets via `[ticket-id]` bracket refs
- CLI: `tk recompute [--project=NAME]` — rebuild commit journal from full git history
- Journal: Auto-close tickets when commits contain `Closes: [id]` or `Fixes: [id]`
- Journal: Diff stats tracking (lines added/removed, files changed) per commit
- Journal: Work duration estimation for live commits
- Journal: Watch cycle runs automatically inside `tk serve` alongside sync

### Fixed
- TUI: Form cursor highlights character at position instead of inserting block that displaced text
- TUI: Picker selection uses background highlight instead of brackets

## [5.5.0] - 2026-03-28

### Added
- MCP: `ticket_store_info` tool returns central store root and per-project ticket directory paths

### Changed
- MCP: `NewServer` accepts `centralRoot` parameter for store info tool

## [5.4.0] - 2026-03-28

### Fixed
- Core: Move `PropagateStage` into `Advance()` — epics now auto-close via MCP when all children reach done
- CLI/MCP: `--repo` flag and MCP `repo` parameter now resolve central store projects instead of only looking for `.tickets/` directories

### Changed
- Core: `AdvanceResult` includes `Propagated []StageChange` for parent stage changes

## [5.3.0] - 2026-03-28

### Added
- Core: `Store` interface extracted from `FileStore` — MCP server and all ticket operations now accept the interface
- Core: `MultiStore` for multi-project ticket storage with namespaced IDs (`project/ticket-id`) and cross-project resolution
- Core: `ParseNamespacedID` and `FormatNamespacedID` utilities for namespaced ticket ID handling
- MCP: Optional `project` parameter on `ticket_create`, `ticket_list`, `ticket_ready`, `ticket_inbox` for multi-project filtering
- MCP: Default project scoping in `--central` mode — resolves from CWD when in a repo, all projects when not
- CLI: `tk serve --central` flag to serve all projects from the central ticket store via MultiStore
- Dev: `.mcp.json` with `tk-dev` and `tk-dev-central` entries for local MCP testing
- Tests: `UpdateSection` round-trip regression tests
- Tests: `Serialize` notes duplication regression test

### Fixed
- CLI: Removed dead notes body-stripping workaround in `tk edit --note` (Parse already handles this)

### Changed
- MCP: `NewServer` accepts a `defaultProject` parameter for CWD-based project scoping

## [5.2.0] - 2026-03-24

### Added
- CLI: `tk setup` command for first-run configuration — sets central store path, creates ~/.ticket/config.yaml
- CLI: `tk status` command — system health overview with version, config paths, data repo state, sync status, and per-project ticket counts
- Core: Config gate — all commands (except setup/help/version) require valid config with central_root set
- Core: Ticket directories at `<central_root>/tickets/<project>` (not directly under central root)

### Changed
- Core: `CentralStoreRoot()` no longer falls back to `~/.tickets` — requires explicit config via `tk setup`
- Core: Git sync scoped to `tickets/` and `config.yaml` only (won't commit unrelated files in data repo)
- Core: Git bootstrap skips `git init` when central store is inside an existing repo

## [5.1.0] - 2026-03-24

### Added
- Core: Background git sync in `tk serve` — automatically stages, commits, and pushes ticket changes every 5s (configurable via `sync_interval`)
- Core: Sync-blocked marker file (`.tk-sync-blocked`) persists across restarts when rebase conflicts occur
- Core: `git_email`, `git_name`, `default_store`, `sync_interval` config fields in `~/.ticket/config.yaml`
- Core: Split config into shared (`<central_root>/config.yaml`) and local (`~/.ticket/config.yaml`) layers for multi-machine support
- CLI: `tk sync` command for manual one-shot ticket git sync
- CLI: `tk setup` command for first-run configuration — sets central store path, creates ~/.ticket/config.yaml
- Core: Config gate — all commands (except setup/help/version) require valid config with central_root set

### Changed
- Core: Central store fallback changed from `~/code/forge-data/tickets` to `~/.tickets` (generic default)
- Core: `bootstrapCentralStoreGit` reads git identity from config before falling back to `tk@local`

## [5.0.0] - 2026-03-22

### Added
- CLI: `tk init` command for central store configuration — register projects with `--store central|local`, auto-detect project names from git remote, bootstrap central store as git repo
- Core: Config-based ticket directory resolution via `~/.ticket/config.yaml` — `TicketsDir()` now checks project config between `TICKETS_DIR` env and walk-up fallback
- Core: `central_root` config field to override default central store location
- Core: Project name sanitization to prevent path traversal
- CLI: `tk show --metadata` flag to display only frontmatter fields and description (omits notes, reviews, relationships)

## [4.3.0] - 2026-03-15

### Added
- TUI: Review flow for verify-stage tickets — press `r` in dashboard to open full review view with git checkout command, PR URL, and acceptance criteria; approve with optional notes or reject with feedback and stage picker
- CLI: Renamed `tk closed` to `tk done` to match pipeline stage terminology

## [4.2.0] - 2026-03-15

### Changed
- Pipeline: default variant now equals normal for all ticket types (adds review stages)
- Pipeline: high/critical-risk bugs get the full feature pipeline (spec, design, design-review, code-review)
- Pipeline: chores now follow feature risk pipelines (were previously flat)
- Pipeline: tasks simplified to backlog → triage → done (research only, no code stages)

### Added
- TUI: Dashboard now shows active type filter on a dedicated filter line (matches pipeline view)
- TUI: Dashboard supports pgup/pgdn for page navigation and mouse scroll wheel

## [4.1.1] - 2026-03-14

### Fixed
- Pipeline: Add `verify` stage to bug/low, task/low, and all chore pipeline variants that previously went directly from implement to done
- Pipeline: Remove dead `implement>done` gate (no pipeline variant uses this transition)

## [4.1.0] - 2026-03-14

### Changed
- TUI: Dashboard tabs redesigned from `all|triage|verify|review` to `backlog|triage|inbox|done|all`
- TUI: Default tab is now `triage` (was `all`)
- TUI: `all` tab shows all active tickets, not just human-actionable ones
- TUI: Verify/review keybindings now context-sensitive based on ticket state, not tab

## [4.0.0] - 2026-03-14

### Added
- Pipeline: `backlog` stage before `triage` for all ticket types. Full pipeline: backlog → triage → spec → design → implement → test → verify → done
- Pipeline: `backlog>triage` gate requiring description, priority, and risk before promotion
- CLI: `tk backlog` command to list tickets in backlog stage
- CLI: `tk edit --stage` flag to directly set a ticket's stage (bypassing pipeline ordering)
- MCP: `ticket_edit` supports `stage` parameter for direct stage assignment
- Gates: `priority_set` and `risk_set` structural checks

### Changed
- Default initial stage for new tickets is now `backlog` (was `triage`)
- `tk ls` excludes backlog tickets by default (use `--stage backlog` to see them)
- `ticket_list` MCP tool excludes backlog tickets by default (use `stage: "backlog"` to see them)
- `ticket_ready` and `ticket_inbox` exclude backlog tickets
- TUI dashboard excludes backlog tickets from inbox view

## [3.2.0] - 2026-03-13

### Added
- Generic extra fields: arbitrary key/value metadata on tickets via `Extra map[string]string` in YAML frontmatter
- CLI: `--set key=value` flag on `create` and `edit` commands (repeatable, blank value removes field)
- CLI: `tk query` JSONL output includes `extra` field for custom metadata
- MCP: `set` parameter on `ticket_create` and `ticket_edit` for extra field CRUD
- Extra fields flattened to top level in all JSON output (CLI query, MCP show/list/create/edit) — no `.extra.` prefix needed
- TUI: Extra fields rendered in detail view after known metadata fields
- Validation: extra field keys allow only `[a-zA-Z0-9_-]`; values reject all YAML indicator characters (`%`, `!`, `&`, `*`, `@`, `` ` ``, `|`, `>`, `'`, `"`, `:`, `#`, `[`, `]`, `{`, `}`) and control characters to prevent YAML parse corruption

## [3.1.0] - 2026-03-12

### Added
- MCP: `ticket_create` supports `repo` parameter for cross-repo ticket creation. Walks up from given path to find `.tickets/` directory, matching CLI `--repo` flag behavior.
- `FindTicketsDir` exported from `pkg/ticket` for shared use by CLI and MCP.
- CLI: Dynamic column widths in `ls`, `ready`, `blocked`, `pipeline`, and `closed` output — columns align based on actual data
- CLI: Color output for headers (bold), priority (P0=red, P1=yellow), and group headers (bold cyan). Respects `NO_COLOR` and non-TTY detection.
- CLI: Redundant column suppression — STAGE hidden when grouped by stage, TYPE hidden when grouped by type, P hidden when grouped by priority

### Changed
- CLI: "STATUS" column renamed to "STAGE" in all list output
- MCP: `ticket_list` returns summary fields only (id, title, stage, review, risk, type, priority, assignee, parent, tags, deps, links, created). Body content (description, design, acceptance_criteria, test_results, notes, reviews) moved to `ticket_show` only. Response shape changed from array to `{tickets, total, offset, limit}` object.

### Fixed
- MCP: `ticket_list` now paginates results (default limit 50) to prevent responses exceeding MCP client token limits. New `offset` and `limit` parameters control pagination.

## [3.0.0] - 2026-03-11

### Added
- `revert` CLI command and `ticket_revert` MCP tool to move tickets backward in the pipeline (e.g., verify → implement when rework is needed). Requires `--to` and `--reason` flags; appends audit note.

### Removed
- `status` field: replaced entirely by `stage` pipeline field. Legacy tickets with `status` auto-migrate on read.
- `tk start`, `tk close`, `tk reopen` commands removed. Use `tk advance` and pipeline stages instead.
- `--status` filter flag removed from `tk ls`. Use `--stage` instead.
- `status` group-by option removed from `tk ls --group-by`.
- `status` field removed from MCP `ticket_create`, `ticket_edit`, and `ticket_show` responses.

### Fixed
- MCP: `ticket_list`, `ticket_ready`, and `ticket_blocked` return `[]` instead of `null` when no tickets match filters
- TUI: Detail view now word-wraps body text, review log entries, and notes to fit terminal width

## [2.7.0] - 2026-03-10

### Changed
- TUI: Unified visual layout across open, edit, and create screens (consistent header style, fixed-width labels, matching indentation)

### Added
- TUI: ctrl+j inserts newlines in multi-line form fields (description, note)
- TUI: ctrl+s saves/submits form in edit and create mode (works from any field, including choice fields)

### Fixed
- MCP: design/acceptance/test_results fields with `## ` markdown headings no longer get truncated on read-back
- Scanner buffer increased from 64KB to 1MB per line in ticket parser, preventing silent failures on large fields
- MCP: `ticket_edit` no longer silently swallows re-read errors after update

## [2.6.1] - 2026-03-07

### Added
- `ticket_create` and `ticket_edit` MCP tools now support `risk` parameter for setting risk level (low, normal, high, critical)

## [2.6.0] - 2026-03-06

### Added
- Pipeline configuration externalized to embedded JSON (`pkg/ticket/pipelines.json`)
- New pipeline stages: `design-review` and `code-review` for explicit review phases
- Risk-based pipeline variants: each ticket type can have different stage sequences per risk level (low, normal, high, critical)
- `PipelineFor()` now accepts optional risk level parameter to select variant pipelines
- Hybrid gate model: structural gates (checked server-side) and agentic gates (declared in config, returned as requirements)
- `EvaluateGates()` returns structured gate results with type, status, and descriptions
- `AllStages()`, `DisplayStages()`, `GateInfoFor()` config accessor functions
- `PipelineDescription()` generates workflow text from config data
- TUI pipeline view colors for new `design-review` and `code-review` stages
- `ticket_pipelines` MCP tool: returns full pipeline config (stages with roles, variants, gates) as structured JSON for orchestrator consumption
- Stage roles in pipeline config: `intake`, `definition`, `work`, `review`, `terminal` — enables orchestrators to dispatch based on stage type
- `ticket_advance` MCP tool now returns structured gate results (name, type, status, description) and accepts `evidence` parameter for agentic gate attestation

### Changed
- Stage validation is now config-driven instead of hardcoded map
- `cmd/pipeline.go` reads stages from config instead of hardcoded list
- TUI pipeline columns read from config instead of hardcoded `allStages`
- `tk workflow` command generates output from pipeline config
- `ticket_workflow` MCP tool generates output from pipeline config
- Risk-based gate scaling (`applyRiskScaling`) removed; replaced by pipeline variants
- Help text updated to reflect new stages and risk-based pipeline variants
- `tk workflow` and `ticket_workflow` now show normal pipeline variants alongside defaults

## [2.5.0] - 2026-03-04

### Added
- TUI `d` key to delete ticket from dashboard with y/n confirmation prompt
- `branch` frontmatter field to track git branch associated with a ticket
- `--branch` flag on `tk edit` to set/clear the branch field
- MCP `ticket_create` and `ticket_edit` support `branch` parameter

### Fixed
- `MoveTicket` now shallow-copies the full struct instead of manually listing fields, preserving Stage, Review, Risk, Branch, Skipped, and Conversations; resets Stage to triage and clears Review on move
- Ticket body accumulated extra blank lines on each save (parse→serialize round-trip)
- TUI dashboard ID column dynamically sized to widest ticket ID instead of hardcoded 24 chars

## [2.4.0] - 2026-02-28

### Added
- TUI `v` key on verify tab advances ticket to next stage; `R` on review tab approves review
- TUI file watcher — auto-reloads tickets when `.tickets/` directory changes (fsnotify with 200ms debounce)
- TUI edit mode (`e`) — edit title, description, type, priority, assignee, stage, and add notes from the form view
- TUI `o` key as alias for `enter` to open ticket detail

### Changed
- TUI default view is now a single-pane inbox with tabbed filters: all, triage, verify, review
- Removed status-based list view and pipeline kanban as default — focused on human decision points
- Pipeline view now supports text search (`/`), priority cycling (`p`), and create (`c`)
- TUI detail view help bar uses consistent `(k)ey` format with `│` separators

### Fixed
- TUI form text fields wrap long text across multiple lines instead of scrolling horizontally off-screen
- TUI form text fields overflowed past terminal width — now truncated with cursor-aware viewport, left/right arrow movement, and home/end support
- MCP `ticket_create` didn't set `created` timestamp — tickets created via MCP had zero-value dates
- TUI list view ID column truncated slug-based IDs — column width now computed dynamically from visible tickets
- TUI pipeline view missing `c` keybinding for create — now matches list view behavior

## [2.3.0] - 2026-02-28

### Fixed
- TUI create form failed with "ticket ID is required" — `handleCreateTicket` was missing `GenerateID()` and `Created` timestamp

## [2.2.0] - 2026-02-27

### Added
- Encouraging messages on empty listing output — ls, ready, blocked, inbox, closed, pipeline, and next show a random message from a pool of 20 when results are empty. `--json` returns `[]`.

### Fixed
- MCP `ticket_create` failed with "ticket ID is required" — handler was missing ID generation, status, and stage initialization
- Notes with `**bold**` markdown lines were split into multiple notes during parsing — `parseNotes` now validates timestamp before flushing
- MCP `ticket_edit` silently dropped description, design, and acceptance fields — handler now uses `UpdateSection` to persist body fields
- MCP gate checks required body sections unreachable via `ticket_edit` — added `test_results` field and exposed `## Test Results` in show output

## [2.1.1] - 2026-02-26

### Fixed
- Homebrew install — use formula (`brews`) instead of cask (`homebrew_casks`) in GoReleaser config

## [2.1.0] - 2026-02-26

### Added
- **`--repo` global flag** — operate on any repo from anywhere (`tk ls --repo ~/code/other-project`). Walks up from the given path to find `.tickets/`, same as CWD resolution. Errors if no `.tickets/` found.
- **Stage pipeline system** — type-dependent stage pipelines replace flat status enum
  - 7 stages: triage → spec → design → implement → test → verify → done
  - Type-dependent pipelines: feature (7), bug (5), task (5), chore (3), epic (4)
- **Gate enforcement** — structural preconditions for stage transitions
  - Risk-scaled gates (low=advisory, normal=standard, high/critical=strict)
  - Mandatory code + impl review gates at implement → test
- **Review system** — ReviewState tracking (pending/approved/rejected) with ReviewRecord audit log
- **Pipeline workflow functions** — `Advance()`, `Skip()`, `SetReview()` in pkg/ticket
- **Stage propagation** — `PropagateStage()` for parent stage advancement based on children
- **Migration** — `MigrateTicket()`/`MigrateAll()` for status → stage conversion
  - Mapping: open→triage, in_progress→implement, needs_testing→test, closed→done
- **Inbox/next-action derivation** — `Inbox()`, `NextAction()`, `Projects()` for workflow visibility
- New Ticket struct fields: Stage, Review, Risk, Skipped, Conversations, Reviews
- Review Log section parsing and serialization in ticket markdown format
- `ValidateStageForType()`, `ValidateGates()` validation functions
- Pipeline helpers: `NextStage()`, `PrevStage()`, `HasStage()`, `StageIndex()`, `IsFinalStage()`
- **CLI commands:** `advance`, `skip`, `review`, `log`, `pipeline`, `inbox`, `next`, `migrate`
- **Edit flags:** `--stage`, `--review`, `--risk` for direct field editing
- **ls --group-by=pipeline** groups tickets by pipeline stage
- **Backward compatibility:** `start`/`close`/`reopen` map to stage equivalents with hint
- **MCP tools:** `ticket_advance`, `ticket_review`, `ticket_skip`, `ticket_migrate`, `ticket_inbox`
- New tickets default to `stage: triage` on creation
- Integration tests for all pipeline commands (188 assertions total)

### Changed
- **Human-readable ticket IDs** — IDs now use up to 3 meaningful words from the title instead of directory-name prefix (e.g., `fix-login-page-fe32` instead of `tic-fe32`). Existing tickets keep their IDs unchanged.
- `GenerateID()` now requires a title argument; stop words (articles, prepositions, etc.) are stripped from the slug
- `Store.Create()` returns an explicit duplicate error instead of relying on hash collision retry
- `ls` defaults to workflow grouping (In Progress / Ready / Blocked). Use `--flat` for the old flat list.
- `ls` shows dep count (`(2 deps)`) instead of full dep ID list (`<- [t-1234, t-5678]`)
- Ticket validation accepts either `status` (legacy) or `stage` (pipeline) — dual support for migration
- format.go writes stage/review/risk/skipped/conversations fields when present
- `show` checks both status and stage for blocker/blocking display
- `ls` excludes `stage: done` tickets from default view
- `printRow` shows stage when available, falls back to status
- Help text updated with pipeline commands and options
- MCP `toJSON` includes stage/review/risk/skipped/conversations/reviews fields

## [2.0.0] - 2026-02-23

Go rewrite. Full CLI parity with bash version plus new capabilities.
Both implementations remain supported and read/write the same ticket format.

### Added
- **Go binary** — cross-platform, single binary distribution via Homebrew and AUR
- **TUI** (`tk ui`) — interactive ticket browser with list/detail views, inline editing, ticket creation
- **MCP server** (`tk serve`) — stdio MCP server for Claude Code integration
- `--json` global flag on all output commands
- `--version` / `-v` flag (version injected at build time via GoReleaser)
- `stats` command — project health dashboard (status/type/priority breakdowns, open ticket age)
- `timeline` command — bar chart of tickets closed by week with `--weeks=N` flag
- `move` command — move tickets between repos with `--recursive` for full subtree moves
- `--group-by` flag for `ls` (workflow, type, status, priority) with `--group` shorthand
- `--note` flag for `edit` as alias for `add-note`
- `--design`, `--acceptance` flags support multiline text (bash awk limitation fixed)
- GoReleaser config for darwin/linux arm64/amd64 builds
- Comprehensive test suite (144 assertions)

### Changed
- ID generation uses nanosecond timestamps + atomic counter (eliminates rapid-create collisions)
- `create` retries with new ID on collision (up to 5 attempts)

### Fixed
- `ls --parent` now correctly filters to children only
- Multiline `--design` and `--acceptance` flags work correctly (bash awk limitation)
- ID collisions when creating multiple tickets per second

## [Unreleased - bash]

### Added
- `list` alias for `ls` command
- `needs_testing` status
- `-s, --status` flag for `edit` command to change ticket status
- Hierarchy gating: `ready` only shows tickets whose parent is `in_progress`
- `--open` flag for `ready` to bypass hierarchy checks
- Status propagation: `needs_testing`/`closed` auto-bubble up parent chain
- `workflow` command outputs guide for LLM context
- `-t, --type` filter flag for `ls` command
- Interactive prompts when `tk create` is run with no arguments
- Support `TICKETS_DIR` environment variable for custom tickets directory location
- `dep cycle` command to detect dependency cycles in open tickets
- `add-note` command for appending timestamped notes to tickets
- `-a, --assignee` filter flag for `ls`, `ready`, `blocked`, and `closed` commands
- `--tags` flag for `create` command to add comma-separated tags
- `-T, --tag` filter flag for `ls`, `ready`, `blocked`, and `closed` commands
- `-P, --priority` filter flag for `ls` command
- `delete` command to remove ticket files

### Changed
- `create` command now displays full ticket details on success instead of just the ID
- `edit` command now uses CLI flags instead of opening $EDITOR

### Removed
- `start`, `testing`, `close`, `reopen`, `status` commands (use `edit -s` instead)

### Fixed
- `update_yaml_field` now works on BSD/macOS (was using GNU sed syntax)

## [0.2.0] - 2026-01-04

### Added
- `--parent` flag for `create` command to set parent ticket
- `link`/`unlink` commands for symmetric ticket relationships
- `show` command displays parent title and linked tickets
- `migrate-beads` now imports parent-child and related dependencies

## [0.1.1] - 2026-01-02

### Fixed
- `edit` command no longer hangs when run in non-TTY environments

## [0.1.0] - 2026-01-02

Initial release.
