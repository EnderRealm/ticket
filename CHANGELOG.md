# Changelog

## [Unreleased]

### Added
- TUI list view: an `EPIC` column between `PRI` and `TYPE` on the inbox, all, and done tabs, showing the 4-char suffix of the ticket's epic (an em-dash when it has none), so a row's epic is visible without opening it. The column sorts like any other via `s`/`S`; tickets with no epic sort last ascending. A `parent` that names no epic in the store — `tk delete` on an epic leaves its children pointing at nothing — reads as epic-less everywhere: em-dash in the column, and the row stays visible on the backlog tab with its own tab count instead of rolling up under an epic the epics tab no longer shows. An epic always keeps its own backlog rollup row, including the sub-epics a store written before the one-level rule can still hold, so its children never roll up under a row that is not drawn.
- `tk audit [--project=NAME]`: report tickets whose `parent` breaks the one-level epic hierarchy — a parent that is not an epic, a parent that does not resolve, a parent in another project, an epic that has a parent, or a parent cycle. It spans the central store, takes `--project` to scope to one project like `tk frontier`, and supports `--json`. Each parent is resolved inside the project that owns the ticket, the same way a write resolves it, so the report and the write path agree on every ticket. A project the audit could not read is named as a warning (and carried in the JSON as `skipped`) rather than dropped, so "No parent violations." never speaks for a store that was only partly read. Read-only: it names what to fix and never rewrites a ticket, so a store can be cleaned before a write trips on it. A second section reports every epic whose stored `status` differs from the one it now derives from its children (JSON `epic_status`), read in the same per-project pass and under the same `--project` scoping: statuses were derived without migrating what was stored, and the derivation happens at the read choke point, so this is the only place the two can still be compared. An epic whose file stores `closed` with no `abandoned` flag is classed `stored-closed` and called out separately — either a hand-close from before the change or a derived `closed` a write carried into the file, which the file cannot tell apart, and `tk edit <id> --status closed` re-records the ones that were real — where the rest are `stale-status`, the drift between a stored value and the children that deriving the status removes. A stored status the store does not recognise is printed quoted: it is read straight off a file another machine may have written.
- TUI edit form: a `Parent` field, between `Status` and `Note`, prefilled with the ticket's parent. Typing an epic repoints it and leaving it empty clears it, validated by the same write the CLI and MCP go through, so the remedy the one-level hierarchy's rejection names is performable in the TUI instead of only naming a field the TUI could not touch.

### Changed
- A ticket's `verify:` command no longer runs through a shell, and runs at all only if its program is on a machine-local allow-list. `tk verify` and MCP `ticket_verify` took the command string verbatim out of a ticket's acceptance criteria and handed it to `sh -c` in the project's repo directory with the full environment — so a bullet reading `verify: rm -rf ~` did exactly that to whoever ran verify on the ticket. Ticket bodies are written by agents and replicate to every machine over the shared store's git remote, which made this a prompt-injection-to-code-execution path with no exotic steps. The command is now split into arguments on whitespace — single and double quotes group an argument, so `-run 'TestA|TestB'` still works — and exec'd directly: nothing is expanded, so `;`, `|`, `&&`, `$(...)`, backticks, globs and `~` are literal characters passed to the program rather than shell syntax, and the second half of a chained command never runs. The program must then exactly match (not by basename — an entry of `go` does not admit `/tmp/evil/go`) an entry in `verify_allow` in `~/.ticket/config.yaml`; anything else is reported as `refused` without running, naming the command and how to permit it. `verify_allow` is read from that machine-local file alone: an entry in the shared `<central_root>/config.yaml` is ignored, because that file syncs over the same remote that would carry a hostile command, and there is no flag, MCP argument or ticket field that grants permission — an agent can run what the machine's owner has already allowed and can authorize nothing further. The control fails closed in both directions: an explicitly empty `verify_allow: []` refuses everything, and a local config that cannot be parsed — half-written, conflicted, unreadable — also refuses everything and says so in the refusal, rather than silently restoring the defaults over a list the user had narrowed. Unset, the list defaults to `go`, `make`, `cargo`, `pytest`. Listing a program is not a claim that the program is safe: it trusts whoever can write a verify line with everything that program can do, including the forms that run code from outside the repo — `go run pkg@version` and `go install pkg@version` fetch and execute a remote module, `cargo install <crate>` fetches a remote crate and executes its `build.rs`, and `make -f <file>` or `make -C <dir>` runs recipes from a file the command line names rather than the repo's Makefile. They are in the default because `go test` is the primary use case and cannot be dropped over them; what the default buys is that a verify line cannot name an arbitrary program, not that the listed ones stay inside the repo. Shells and interpreters are absent because they remove even that while buying nothing back — `sh -c` or `python3 -c` runs whatever string the ticket supplied — and `swift` is absent on that rule rather than as a build driver, since `swift -e '<code>'` runs arbitrary Swift with the full standard library and needs no remote module to do it; a Swift project gets `swift test` back by adding `swift` to `verify_allow` itself, accepting that a verify line can then run any Swift the ticket names. `npm`, `pnpm` and `yarn` are absent because `npm exec`, `pnpm dlx` and `yarn dlx` fetch and run an arbitrary registry package as a documented feature; a JS project adding one back is opting into a verify line being able to run any published package. `refused` is a fourth status alongside pass/fail/unverified, counted separately in the summary, the `--json`/MCP report (`summary.refused`, and `ok` is false) and the recorded Test Results — a criterion that never ran is not one that disagreed. Control characters — C0/C1, DEL and the Unicode format characters, bidi overrides among them — are stripped from a criterion's text as well as its command before either is printed or recorded, so an escape planted anywhere in the acceptance criteria can neither repaint the terminal the verdict is being judged on nor replay out of the recorded Test Results on every later read; tab is exempt, carrying no repaint risk and being ordinary in a hand-written bullet, while newline and carriage return are not, because the record is one line per criterion. A command's own output is left raw so a test runner's colours survive. `ticket.RunVerify` (exported library API) takes two new parameters, the allow-list and the error that made it unreadable, so an external caller must supply them — pass `project.VerifyAllow()`'s two results to get the CLI's behaviour. Commands that were already shell syntax rather than a plain invocation need rewriting: quote the shell fragment and name the shell (`/bin/sh -c 'a; b'`) with that shell added to `verify_allow`. No ticket in the store carried a verify command when this shipped, so nothing was silently disabled.
- An epic's status is now derived from its children instead of stored: no children reads `backlog`, any child open reads `open`, every child done reads `done`, every child terminal with at least one closed reads `closed`, and anything else reads `backlog`. `done` and `closed` split the terminal case because they say different things — `done` means the work completed, `closed` means it did not — so an epic whose children were every one abandoned or moved to another repo reads `closed` rather than claiming to have finished. An epic never reads `ready` — `ready` means "available to pick up" and an epic is not picked up directly, so epics no longer appear in `tk frontier` or the frontier half of `ticket_ready`. Derivation happens at the store read choke point (`FileStore.List`/`Get`, which `MultiStore` delegates to), so `tk ls`, `tk show`, `tk query`, the TUI tabs, and every MCP response agree without a display site of its own. An epic's `completed` date is derived with it — the date its last child reached a terminal state, blank while the epic is not terminal — where before nothing wrote an epic when its last child finished, leaving the COMPLETED and DURATION columns empty on a finished epic and a stale date beside one a reopened child had made live again; an epic's file now stores no `completed` at all, so no date the writer's clock produced can surface beside one. The one thing about an epic still asserted rather than derived is the intent to abandon it, and it lives in a new `abandoned: true` frontmatter field rather than in `status`: setting an epic's status to `closed` through `tk edit`, the TUI or MCP `ticket_edit` records the flag and closes every non-terminal child in the same action (children that already finished keep their `done`), and setting any other status takes the intent back. The children the abandon closed are reported with the edit — named on `tk edit`'s and the TUI's status line, returned by MCP `ticket_edit` as `closed_children` — so a write that mutated other tickets says so on success as well as in the failure case, where the error names which children were closed and which were not rather than reporting success. Changing a ticket's type to `epic` is judged the same way as any other edit: `tk edit <id> --type epic` on its own carries the status the ticket was read with and is not read as a decision, while a status set in the same call is a status set on the epic that edit makes — `closed` abandons it, and anything else is refused. Reopening a child of an abandoned epic un-closes the epic — the flag is honoured only while every child is terminal — and finishing the child brings it back. Any other status set by hand on an epic that is not abandoned is refused, naming the epic and what it currently reads. Only a status a writer actually set counts as either decision — `tk edit --status`, MCP `ticket_edit`'s `status` argument, or the TUI form's status field cycled off the value it was opened with — and each edit path carries that signal into the store rather than the store guessing at it from the value: a status that merely rode along with an edit to some other field is not judged at all, so it can neither record an abandon nor be refused for disagreeing with children that moved between the read and the write. Because the intent is a stored flag rather than a value read off the children, an edit to any other field of an epic can neither drop it nor invent one: an epic's `status:` field is now advisory, never consulted by the derivation, so what a read-modify-write carries back is inert wherever it lands. Writes tk makes on its own behalf express no intent either: `tk move` closes an epic in the source repo to record that it left, which is not an abandon (a non-recursive move leaves the children staying behind exactly as they were, and the moved-away epic reads as whatever children stayed with it), and the commit-journal watcher no longer auto-closes an epic at all — a commit carrying `Closes: [<epic-id>]` or `Fixes: [<epic-id>]` is reported as a skipped warning and excluded from the closed count, since writing the epic would change nothing an epic's children do not already say. This replaces the epic-done guard and the parent-status propagation that kept the stored value in sync, both of which are gone: a value defended by a guard on every write path had already fallen out of sync twice. **Existing stores are not mutated and no stored status is migrated** — the derivation ignores it, so an epic that was closed by hand before this change reads as its children imply (`backlog` for a childless one) until it is closed again, which records the flag. Treating a legacy stored `closed` as the intent at read time was rejected deliberately: an epic that merely derives `closed` has that value written into its file by the next unrelated edit, and reading it back as an assertion would forge exactly the intent this design separates out. Re-run `tk edit <epic> --status closed` on any epic that should stay abandoned; every other epic whose displayed status moves was drift between the stored value and the children, which is what this removes. `tk audit` lists both — every epic reading a different status than its file stores, with the ones storing `closed` and no flag called out separately as `stored-closed` — so no epic's move has to be discovered. Re-record those before editing them: the stored value is the only surviving trace of the decision, and the next write of the epic replaces it with the derived one. Neither drift class is confined to the migration, and the report says so: every write of an epic stores the status it derived at that moment, which the next change to a child makes stale, and an epic that derived `closed` when it was written comes back as `stored-closed` — so a stored value is evidence of a decision only on a file older than this change.
- TUI: the epics tab is now a grouping over the shared column table rather than a parallel tree model, and one predicate decides tab membership for every tab. Each tab's show/hide rules were written out in three places — the dashboard's row builder, the epics view's own rules, and a hand-written mirror behind the tab-bar counts — and had drifted: the counts skipped the empty-status guard, so a ticket with no status inflated the `all` count above the rows it labelled, and ignored the type and text filters, so every count froze at its unfiltered total the moment a filter was typed. Counts now come from the same code path the rows do, so a tab bar always reports what its tab renders, filtered or not (on the epics tab it counts epics: children are nested inside a group that is already counted, so expanding one does not inflate it). Expand/collapse, jumping to an epic from a backlog rollup, opening a child, the per-epic progress bar, and the epics tab's AGE-descending default sort all survive the fold; search, type filter, and the row commands (`p`, `m`, `d`, `y`, `w`) now work on the epics tab like any other. The `EPIC` column is dropped from the backlog and epics tabs, where it could only ever render an em-dash or repeat the epic drawn directly above the row. The epics tab's "No epics found." becomes a shared "No tickets found." placeholder that every tab draws when nothing passes its rules and filters, instead of only the epics tab having an empty state.
- The ticket hierarchy is now one level deep and enforced on write: a ticket's `parent`, when set, must resolve to an epic in the same project, and an epic itself cannot have a parent (sub-epics are no longer representable). Enforcement lives at the store write choke point, so CLI, MCP, and TUI writes are all covered, and the error names the offending parent and its actual type. A parent in another project is refused by name rather than surfacing as a confusing "not found" — a per-project store cannot resolve one, and an epic and its children are meant to live together. Parents resolve through the same matching as everything else, so full, partial (`eb6c`), and namespaced (`project/epic-abcd`) forms all work, and the resolved epic's ID is what gets stored — a partial form is not kept verbatim, where it would name an epic the write accepted and no view could find. Existing stores are never mutated: tickets that break the rule still load, list, and render, but writing one back is refused until its `parent` is cleared or repointed — run `tk audit` to find them. This also gives "an epic's children" a single definition: the backlog tab's rollup count and the epics tab's expansion previously disagreed for nested epics (an epic transitively holding 17 tickets showed 0 on the backlog tab), and both now report the same direct children.
- MCP `ticket_edit` clears a `parent` when passed an empty string, where it previously read that as "no change". Clearing is half the remedy the validation error names, and it was the half an agent could not perform. Omitting the field still means "no change"; `tk edit --parent=''` was already able to clear it.

## [7.7.1] - 2026-08-03

### Fixed
- A ticket ID carrying path separators can no longer read, write, or delete files outside the store directory. A ticket ID becomes its filename, and `filepath.Join` cleans traversal segments instead of failing, so a ticket file declaring `id: ../../evil` — hand-edited, committed to a shared repo, synced in by another tool — silently resolved outside `.tickets/`: `tk edit` overwrote whatever `.md` file sat there, `tk create` wrote a new one, and `tk delete` removed it (the last two reachable on a central-store project via `project/../../evil`). On the central store the project half of a namespaced ID escaped the same way: `../evil` rooted that project's store above `<central_root>/tickets`, so `tk serve`'s `ticket_show`, `ticket_edit`, and `ticket_delete` — and parent-epic propagation following whatever a ticket's `parent` field named — reached `<central_root>/evil.md`. A store path is now bounded at both halves: the ID must be a bare filename, and the project must be a bare directory name, which is the rule the central store already applied to project names coming from config, git remotes, and directory basenames. `tk` reports `invalid ticket ID "../../evil" in <dir>` or `invalid project ".." in <dir>`, naming the offending value and the directory it was resolved against so the source can be found and fixed. A project named `.` is rejected on the same grounds — it collapses onto the tickets root rather than a project directory — so `tk init --project .` now errors instead of registering a project rooted there. IDs `tk` generates are unaffected — they are always slug-plus-hash with no separators.
- Parent-epic propagation now fires for every write against a central-store project, not just those made through `tk serve`. Direct CLI writes (`tk edit`, `tk create`, `tk dep`, `tk link`, `tk move`, ...) and TUI writes from `tk ui` built their store over the central project directory without naming the project, so a namespaced `parent` (`project/epic-abcd`) never resolved and the epic silently never advanced or rolled up — the 7.7.0 entry below called these out as not yet covered. MCP `ticket_create` with a `repo` override now scopes its store the same way, keeping the override in step with the default path (creating a ticket does not itself propagate). The "epic can't be done with open children" guard was unaffected on these paths: it finds children by scanning the store with a bare-vs-namespaced tolerant compare, so it already tripped on a direct CLI or TUI write; it now also runs on the parent writes propagation performs. A store built over a local `.tickets/` directory still carries no project namespace and continues to reject namespaced IDs.
- `tk ls --parent <id>`, the parent and `## Children` lines of `tk show`, the TUI epics tree, and the TUI backlog tab's epic rollup now recognize children whose stored `parent` is namespaced. All compared `parent` byte-for-byte, so a child recording `project/epic-abcd` was invisible under the epic's bare ID while its bare-parent siblings showed — on the backlog tab it also escaped the rollup, listing as a loose row and missing from the epic's child count. Both ID forms now match.
- `tk move -r <id> <repo>` no longer leaves descendants behind. The descendant walk indexed children by their raw `parent`, so on a central-store project every child recording `project/epic-abcd` — and everything beneath it — was skipped while its bare-parent siblings moved, reporting success on a half-migrated epic. The walk now normalizes the seed, the index keys, and the queued IDs to the bare form, and a visited set bounds it so a `parent` cycle terminates instead of looping forever. A `parent` naming a *different* project is skipped rather than normalized: stripping the prefix indexed such a child under this project's same-suffix ticket, so a recursive move of that ticket carried the child along — closing it in the source and copying it into the target repo. A local `.tickets/` store carries no project, so every namespaced `parent` is foreign to it, matching the IDs its store already refuses to resolve. The old ID → new ID remap the move applies had the same flaw: a namespaced `parent`, dep, or link naming a ticket that was itself moving missed the lookup, so the moved ticket landed detached from its new parent and its deps and links were reported stripped and dropped. That map is now bare-keyed on both sides, so all three reference kinds are remapped to the new IDs.
- `ticket.Projects()` (exported library API, no callers in the `tk` binary) now counts children whose stored `parent` is namespaced in its epic rollups; they were dropped from the child total, status breakdown, and next actions.
- `tk move` now marks the source ticket `closed` rather than `done`. A moved ticket did not complete in this repo, it left, so `done` asserted something false — and it broke two things. `tk move -r <epic> <repo>` failed on any epic with a non-terminal child: the root is processed first, and marking it done tripped the "epic can't be done with open children" guard *after* the target copy had already been written, leaving the target dirty and the move neither done nor undone. Recursive move therefore only worked on a fully-closed epic. Separately, moving away the last non-terminal child of an epic that stays behind rolled that epic up to done, as if the work had finished rather than relocated; `closed` propagates nothing. Note that moved tickets are now hidden from a default `tk ls`, which lists non-closed tickets only — filter with `--status=closed` to see them. Both statuses are terminal, so blocked/ready and epic-rollup calculations are otherwise unchanged.
- A write to a project that is neither registered as `store: central` in `config.yaml` nor already has a directory under `<central_root>/tickets/` is now refused instead of creating one, and a project directory that exists on disk but is not registered as `store: central` is now listed, marked unregistered, instead of silently ignored. `FileStore.Create` ensures its own store directory, so on the central store whatever project name a ticket ID carried conjured a matching directory: `tk serve` started in a scratch directory resolved its default project from that directory's name, and the first `ticket_create` planted `<central_root>/tickets/<uuid>/`, which the background sync then committed and pushed to the shared remote. `tk init` creates a project's directory before registering it, so registered projects are unaffected — including one whose directory is absent because it has never held a ticket and git tracks no empty directories, as on a freshly cloned central store, where the write creates it; every other name now errors with ``project "<name>" has no ticket directory in <root> — run `tk init` in that project's repo to create it``. The existence check does not follow symlinks: a symlink or file where the project directory belongs is refused rather than written through, since the write would land outside the store and the listing walk (which does not follow it either) would never show those tickets. Separately, tickets already sitting in an unregistered project's central directory were invisible: `tk ls` and `tk ui` required `store: central` in `config.yaml` and otherwise fell through to a local `.tickets/` search, reporting an empty store while the files sat on disk (five loom tickets went unseen for weeks this way). Resolution now falls back to an unregistered project's central directory when that directory exists on disk and the repo has no `.tickets/` of its own — the name is inferred from a git remote or directory basename, so it must not shadow a store the repo actually has, which also leaves a project deliberately registered with a non-central store on the store it has. Registration is one predicate on every side — an entry in config carrying `store: central`, not merely an entry: `store` lives in the shared config alone, so a project whose shared config is missing (never cloned, or lost in a sync) keeps only its local `path`, and keying on presence would have sent exactly that project back down the local search this fixes. The fallback `Lstat`s the directory for the same reason the write does — this resolution feeds writes too, so following a symlink would land a `tk create` where `MultiStore.Create` refuses to write. Every surface marks the state: `tk ls` and the other CLI commands warn on stderr, `tk ui` marks the header for the session (dropping the marker rather than wrapping the row when the terminal is too narrow to hold it), `ticket_list` returns `unregistered_projects` naming the unregistered projects across the whole filtered result set rather than the requested page — like `total`, so the answer does not depend on which page was asked for — and `ticket_store_info` returns an `unregistered` list alongside the project paths, and still returns those paths, with a note in place of the list, when config cannot be read at all. Nothing is written or registered — that remains `tk init`'s job. Ticket files nested one level deeper than a project directory (`<central_root>/tickets/<project>/<project>/*.md`) stay unreachable — directory-level listing looks one level down, not two.
- A `tk move` that fails partway now reports what landed instead of discarding it. The move is not atomic and is not rolled back: `MoveTicket` returns the results for the tickets that completed (created in the target *and* closed in the source) alongside the error, `tk move` prints them, and the error names any ticket written to the target whose source copy is still open — the one state that needs reconciling by hand. Previously all of it was dropped on the floor and the user was left to diff two repos.

## [7.7.0] - 2026-08-02

### Added
- Dependency cargo: a dep edge can name what concretely flows across it (a branch, a schema, a doc) via `tk dep <id> <dep-id> --cargo "<what flows>"` or the optional `cargo` argument on `ticket_dep`. Annotations live in a `dep-cargo` frontmatter block keyed by dep ID; existing bare deps are untouched and the annotation is optional everywhere. Passing `--cargo ""` clears an existing annotation. `tk dep tree` renders `← carries: <cargo>` per edge and marks unannotated edges `← no cargo`, so fake dependencies are visible during grooming. `tk show` renders a `## Dep Cargo` section, and `tk query`/MCP responses carry `dep_cargo`. Removing a dep drops its cargo; moving a ticket remaps cargo to the new dep IDs.
- Ticket outputs: an `outputs` frontmatter block recording what a ticket produced (branch, commit, artifacts, freeform key/values), so a downstream ticket can read its dependency's results instead of re-deriving them from notes. Set keys with `tk edit --output key=value` (blank value removes) or the `outputs` argument on `ticket_edit`; `tk show` renders an `## Outputs` section and `tk query`/MCP responses carry `outputs`. Landing a ticket populates it automatically — the commit watcher records the closing commit's SHA and branch on auto-close, and writing a ticket at status `done` copies its `branch` field (done at the store write choke point, so CLI, MCP, and TUI all get it). Existing values are never overwritten, and a derived value that would not serialize cleanly (e.g. a `%wip` branch name) is dropped rather than written.
- Verifiable acceptance criteria: a criterion can declare its check as a `verify: <command>` line indented under its bullet. `tk verify <id>` and the `ticket_verify` MCP tool run each declared command with `sh -c` in the ticket's project directory (120s timeout per command, output capped at 4KB), report per-criterion pass/fail with exit codes, and record the run in the ticket's Test Results section. Criteria with no command are reported as unverified, not failed; `tk verify` exits non-zero only when a command fails.
- `tk frontier` and the `ticket_frontier` MCP tool: list the schedulable set — tickets with status `ready` whose dependencies are all done or closed. A ready ticket with an unresolved (or missing) dep is excluded. `tk frontier` spans the central store and takes `--project` to scope to one project; `ticket_frontier` takes `project` and `tag`.
- `ticket_delete` MCP tool: permanently delete a ticket by ID (supports partial matching), mirroring `tk delete <id>`. It resolves the ID, hard-deletes the ticket file, and returns the resolved ID that was deleted. This is distinct from setting a ticket's status to closed.
- TUI `w` spawn: the new iTerm window is now named `PROJECT -- ID4 -- TITLE` (uppercased project, the ticket's 4-char id suffix, title truncated to 20 characters) so each worker is identifiable. The default template reasserts the title via a `printf` OSC escape after the shell's prompt hook and `export`s `CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1` (exported, not prefixed inline, so it survives a `claude` shell alias), so the name persists instead of being overwritten by the shell or by Claude Code. New `spawn_command` placeholders `{project}`, `{title}`, and the pre-sanitized window-name token `{wtitle}` support custom templates.
- Filter tickets by a custom extra field with substring matching: `tk ls --field env=prod` (matches `env=production`) and the `ticket_list` MCP tool's `field` parameter. A ticket matches only when the key exists in its extra fields and the stored value contains the filter value.
- TUI list view: single-key status changes for grooming. On the backlog tab `r` moves the selected ticket to `ready`; on the inbox tab `b` moves it to `backlog` and `x` marks it `done`. The footer advertises each key only on the tab where it applies.

### Fixed
- Parent-epic propagation and the "epic can't be done with open children" guard now fire for central-store writes made by `tk serve` — every MCP update, plus commit-triggered auto-close from the `tk serve` watch loop or `tk watch`. Direct CLI and TUI writes against a central-store project (`tk status`, `tk edit`, `tk ui`) build their store without a project namespace and are not yet covered. A ticket's `parent` field stays namespaced (`project/epic-abcd`) when the write is delegated to the project store, so the parent lookup missed and the child-vs-parent compare never matched — epics silently never advanced or rolled up, and the guard never tripped. A project store now resolves IDs namespaced under its own project, and parent matching tolerates bare-vs-namespaced IDs. A parent in a different project still does not resolve (and so does not propagate), rather than mis-resolving to a same-suffix ticket in the child's project.
- Empty or whitespace-only ticket ID now returns an "id is required" error instead of partial-matching every ticket (in a single-ticket store it silently resolved to the lone ticket — dangerous for `tk delete` and MCP ticket_delete).
- ticket_list / tk ls: default (no status filter) now returns all non-closed tickets instead of keeping closed and dropping done+backlog; epics browsed by parent no longer hide their non-closed children.
- TUI epics tab: `e` now opens the edit form for the selected row — an epic, or an expanded child ticket — so epics are editable from the TUI. Previously the epics tab had no edit key (only the dashboard tabs did), leaving no way to edit an epic. The footer now advertises `(e)dit`. Backlog-tab epic rollups were already editable via the shared dashboard `e` handler.
- TUI footer: the type-filter hint now renders as `(t)ype`, embedding the key letter into the word to match the convention used by every other hint (`(s)ort`, `(c)reate`, `(q)uit`), instead of the inconsistent `(t) type`.
- TUI footer: with no type filter active, the segment now reads `type: all` instead of `all types`, matching the `type: <value>` format shown when a filter is set.
- TUI footer: collapsed the separate `(t)ype` key hint and `type: <value>` status into a single `(t)ype: <filter>` segment.

## [7.6.0] - 2026-06-08

### Added
- TUI create form: a Status field limited to `backlog` (default) and `ready`, so a new ticket can be filed straight to `ready` and skip the backlog grooming round-trip. Edit mode's full five-status selector is unchanged.

### Changed
- TUI create/edit form: Enter now advances through fields and saves on the last field, instead of submitting from the Title field. Newlines in multiline fields are entered via ctrl+j; ctrl+s still saves. The footer surfaces these hints contextually (`ctrl+j newline` on multiline fields, `enter save` on the last field).

### Fixed
- `ticket_ready`, `ticket_blocked`, and `ticket_inbox` MCP tools now return slim summary-shaped tickets (matching `ticket_list`) instead of full bodies with unbounded note histories. They previously emitted `description`/`design`/`acceptance_criteria`/`test_results` and every note untrimmed, wasting tokens on board-survey calls that only consume summary fields. `ticket_inbox` keeps its `action`/`detail` wrapper.
- TUI: command-bar footers (dashboard, epics, create/edit form, detail view) now wrap across two or more lines when the terminal is too narrow to fit them on one line, instead of clipping trailing commands like `(q)uit`. The extra footer rows are reserved in each view's height math so content is never pushed off-screen.
- TUI dashboard: the CREATED/MODIFIED/AGE time-column text is now legible on the selected row. It was rendered in `colorSubtle` (`#4e4e4e`), nearly identical to the selection background `colorSurface` (`#4a4a4a`), making it invisible under the selection bar; it now brightens to `colorGray` when selected.

## [7.5.1] - 2026-06-07

### Fixed
- `ticket_blocked` MCP tool now scopes results to the default or explicit `project` in multi-project mode, matching `ticket_ready`/`ticket_list`/`ticket_search`. It previously ignored the `project` argument and returned blocked tickets across all projects.

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
