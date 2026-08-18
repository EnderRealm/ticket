# Changelog

## [Unreleased]

### Fixed
- The journal loops in `tk watch run` and `tk serve` quote project names in every per-project log line, so control characters from shared-config project keys cannot forge log entries or inject terminal escapes when `tk watch logs` displays the file.

## [8.0.0] - 2026-08-16

### Added
- An opt-in `auto_retrospect` flag per project in the shared `<central_root>/config.yaml`: with it on, the journal watch cycle hands each newly closed ticket to `loom retrospect <project>/<id>`, which mines that ticket's sessions into knowledge candidates. The trigger sits on the watch cycle rather than in whichever tool closed the ticket, so it catches every close — a commit `Closes:`, `tk edit`, the TUI, an MCP write — by scanning the store for tickets reading `done` or `closed` that carry no marker in `~/.ticket/state/<project>/retrospects.jsonl` yet. The flag is off by default and set by hand: `tk init` does not write it and the journal-defaults migration does not touch it, since tk must not depend on loom being installed. Enabling it does not mine the store's history — the first cycle records every already-closed ticket without firing and logs that it did, so only closes after enablement are mined. Epics are skipped, an epic's `done` being derived from children that each fire their own retrospect. The marker is keyed on the ticket ID alone, so a reopened and re-closed ticket is never mined twice. What a cycle can start is bounded: at most four runs, with the rest deferred to later cycles and the truncation logged, and only a plain ticket ID under a plain project name is handed to a child process, since both halves arrive over a git remote — the ID in ticket YAML, the project name as a key of the shared config. The read-scan-append window is serialised across processes by an flock beside the marker file, because a machine runs a journal loop per `tk serve` alongside `tk watch` and two of them would otherwise fire the same close. Every part of it is best-effort: the marker is written before the process starts (a lost retrospect beats a duplicate extraction), the run is started and reaped off the cycle so an LLM extraction never stalls the watcher, a failing `loom` is a log line, and a `loom` missing from `PATH` records nothing at all, so the pending closes fire on the first cycle after it is installed. `tk watch status` lists the flag beside `auto_link` and `auto_close` in both text and JSON, and the cycle now runs for a project that has only this flag on.
- An append-only mutation journal at `~/.ticket/state/<project>/mutations.jsonl`, a sibling of the commit journal: one JSON line per ticket change carrying `timestamp`, `ticket_id`, `operation` (create, edit, add-note, dep, link, delete, move), `source` and `fields_changed`. It is the audit trail git history cannot give — a ticket changes far more often than the store is committed, and a commit records the store's state rather than the writers that produced it. The hook sits at the store layer (`FileStore.createLocked`, `updateLocked`, `Delete`, and `MoveTicket` for the move itself), so CLI, MCP and TUI writes are all covered without a single instrumented call site, and the operation is derived from what an update actually changed rather than from which command was run. A move logs a `move` entry in each project's log, alongside the entries its writes produce mechanically — `create` in the destination, `edit` in the source where the original is closed — and the `move` lines are the semantic record of what happened. A failed append never fails the mutation: the write is already on disk, so a broken trail is a warning on stderr and nothing more. The log is the one state path `TK_STORE_ROOT` moves — the commit-journal commands are refused outright under the override, but a mutation is appended by every write and cannot be refused, so an isolated sandbox would otherwise file its throwaway tickets in the machine's real trail.
- Source attribution on every mutation: `TK_SOURCE` wins wherever it is set, then the source the store was wrapped with, then `human`. The MCP write tools (`ticket_create`, `ticket_edit`, `ticket_delete`, `ticket_add_note`, `ticket_dep`, `ticket_link`) take an optional `source` argument and otherwise record the client name from the MCP handshake (`mcp` for a client that declares none); the journal watcher's auto-closes record `watch`, since a daemon acting on a commit is not a person editing a ticket.

### Fixed
- The state-path helpers no longer join a project name into `~/.ticket/state/` unchecked. `state.Dir` and the override branch behind `MutationLogPath` and `RetrospectLogPath` joined the name straight in, and `filepath.Join` cleans `../` segments rather than failing, so a shared-config `projects` key like `../../elsewhere` — the map the watch loops iterate, replicated over git from other machines — resolved a state tree outside the state root, which the watch cycle then created on every tick before it ever checked the repo for a `.git`. The check now sits inside `internal/state` on the one join every state-path helper (and `journal.JournalPath` through it) resolves through, the way `project.CentralProjectDir` bounds the store-path join, so no call site repeats it; the watch loops report the refused project through the error paths they already log. Paths for a valid name are unchanged, under `TK_STORE_ROOT` as well as under `HOME`.
- The commit-journal loops in `tk watch run` and `tk serve` no longer join a config project name into a store path unchecked. Both iterate the shared config's `projects` map — whose keys arrive over git from other machines — and built the watch cycle's store from the key with no `project.ValidName` check, and `filepath.Join` cleans `../` segments rather than failing, so a crafted key resolved the store an auto-close writes into outside the central tickets root. The bound now lives in `project.CentralProjectDir` itself, the one place a project name is joined into `<central_root>/tickets/`, so every caller inherits it rather than the check being copied a third time; the loops report the refused project by name and carry on with the rest, since a project that silently stops being journalled is indistinguishable from one with no commits. A project that is not centrally registered is unaffected — its commits are still journalled and only the auto-close that needs a store is skipped.
- The TUI's `w` spawn no longer interpolates `{dir}` and `{project}` into the `sh -c` template raw. `{dir}` lands inside the default template's innermost quoting (`'"'"'`, within the `osascript -e '...'` wrapper), so a project path carrying a single quote closed that layer and the remainder parsed as shell — the same hole already closed for `{id}` and `{title}`, left open on the value nested deepest. The old "machine-local config, so trusted" reasoning had also stopped holding: `{dir}` can be the `--repo` value or the process working directory when the config records no path for the project, and under `TK_STORE_ROOT` the config half is the override root's file, while `project.ValidName` bounds a project name for path joining and says nothing about shell syntax. Both are now refused at the same boundary `{id}` is, on the characters `{title}` is sanitized of — `'`, `"`, `\`, `$`, backtick, `!`, and control and Cf runes — returning an error and no command, surfaced as the existing one-row `refusing to spawn` status. Neither value is ever altered: a path with a character replaced names a different directory, so `cd` would land somewhere the operator did not choose while the spawn reported success. Ordinary paths are untouched, spaces included.
- A valueless `verify_allow:` in `~/.ticket/config.yaml` refuses everything instead of restoring the defaults. The allow-list keeps three states apart — absent falls back to `go`, `make`, `cargo`, `pytest`, while present-but-empty refuses everything — but YAML resolves a bare key (and `~`, and `null`) to null, and yaml.v3 resolves null before it consults a field's own unmarshaler, so the key decoded to the same nil slice an absent key does. Someone hardening a machine by writing the key with no value silently got the defaults back: the one control standing between a synced ticket body and command execution failed open on a reasonable spelling of "allow nothing". Writing the key at all is now intent to refuse, recovered from the raw node during the config decode, so all three spellings mean what they read as and the distinction survives a load/save round trip in both directions.
- A ticket file that fails to parse is no longer dropped without a word, and can no longer make an epic read finished over live work. Deriving an epic's status from its children gave a swallowed parse error a new blast radius: a child that failed to parse left its epic's derivation, so one malformed file could make the epic read `done` or `closed` while open work sat underneath it — and `tk audit` could not surface it either, reading through the same listing. The trigger needed no corruption, only a typed field carrying a value tk would never write: `abandoned: maybe` in frontmatter another machine pushed took the whole ticket down, as did a bad `priority` or `created`. Both halves are now handled by class. A type mismatch on a typed field is tolerated — yaml.v3 reports one and decodes every other field regardless, so a bad value costs that field and the ticket still loads, lists, and counts toward its epic; the three date fields are read off the raw frontmatter map for the same reason, since a value that is not a date aborts the decode outright rather than reporting a type error. Leniency covers only the fields whose loss stays inside the one ticket. It stops on two counts, both of which would otherwise have reopened the hole this fixes. A decode that produced no ID produced no ticket: yaml.v3 reports a repeated top-level key as a type error too, but finds it before decoding any field, so a doubled `status:` from a hand-resolved merge conflict would have parsed to a blank ticket rather than an error. And a lost *load-bearing* field is other tickets' business, or the derivation's: `parent: [epic-1111]` decodes to no parent, dropping the ticket out of its epic's children with nothing on the ticket to show it; `deps: notalist` silently empties the dependencies so the ticket reads unblocked in `tk ready` and `tk frontier`; and `type: [epic]` on an epic's own file leaves it typeless, so the derivation passes over it and it renders the stale status its file stores — often a `done` a previous write baked in — with the demotion bypassed entirely. A value the frontmatter carried and the decode dropped is refused, while `parent:`, `parent: ""`, `deps: []` and `type: ""` are a writer saying "none" and stay readable. A tolerated mismatch the raw pass could not vet — a complex top-level key errors that pass while the struct pass carries on — falls back to strict rather than being granted unchecked. Anything left — broken YAML, frontmatter that never closes, a duplicated key, a dropped parent, dep or type, an unreadable file — still fails, and the skip is now loud: `FileStore.List` warns through `ticket.Warnf` naming the project and file, and while such a file stands, no epic in that project derives `done` or `closed`, because the missing ticket could be any epic's child. The demotion is applied on every path that derives or compares a derived status — `List`, `Get`, the audit, and the write-path checks that quote a derived status back at the writer — so a single read, a listing and the audit agree. `tk audit` carries the files as `skipped_files` in JSON and names them inside the "this report is incomplete" warning that already covered projects it could not read.
- The watcher's auto-close resolves a commit ref strictly. A store lookup falls back to substring matching on ticket filenames, so a bracketed token that names no ticket at all — `Fixes: [auth]` — closed an unrelated ticket and appended a note to it whenever exactly one filename contained the token. With `auto_close` on by default across every registered project, and a commit subject not fully under the store owner's control in a repo taking outside contributions, that made commit-message text a live write trigger. Only a ref naming a ticket exactly (bare, or namespaced to the project being journalled) now closes one; anything matched by substring is a warning naming the ref and what it matched. Interactive surfaces keep the partial matching — a human is there to see what it resolved to — and the journal entry is unchanged, since it records what the commit said and writes no ticket state.
- The commit journal actually runs. `tk init` hardcoded `auto_link: false` and `auto_close: false` on every registration, and the watch cycle skips a project with both off, so the daemon walked past all twelve registered projects and journalled nothing — a default nobody chose, which made the `[ticket-id]` commit discipline inert and left the only commit→ticket audit trail empty. Registration now writes both flags `true`, and a store registered before that is migrated once: the first watcher to open it (`tk watch run`, or the journal loop `tk serve` starts) flips every project carrying *both* flags false — exactly the state init wrote — to true, and records `journal_defaults_migrated: true` in the shared `<central_root>/config.yaml`. A mixed pair is a deliberate link-only or close-only choice and is untouched, and the marker makes the flip once-ever, so journaling turned off after it stays off. The marker lives in the shared config beside the flags it decided, so the migration travels with the store rather than re-running on every machine that reads it, and neither entry point runs it under `TK_STORE_ROOT` — watch refuses to run there and serve starts no journal loop.
- The watcher matches namespaced ticket refs. Its bracket pattern predated namespaced IDs, so `[warp/dashboard-foo-65c0]` — the form the central store hands every agent, and the form most commit subjects therefore carry — matched nothing at all: a project committing that way journalled no entries and auto-closed no tickets even with both flags on. A ref may now carry a single `project/` prefix. A ref naming the project being journalled is recorded and closed under its bare ID, since that project's journal file and its store are both scoped to it and every entry written before namespaced IDs existed is bare; a ref naming a different project passes through unchanged and is reported by the existing auto-close warning, because a project-scoped store cannot resolve it. `tk recompute` resolves refs the same way — it rebuilds the file the watcher appends to, so keying it differently would split one ticket's history in two — and a commit naming one ticket in both forms gets a single entry, carrying the close.
- Whether the watcher journals anything is visible rather than silent. `tk watch run` and the journal loop inside `tk serve` both log `projects=N journaling=N skipped=[...]` at startup and again whenever a config reload changes the set, and `tk watch status` lists every project with its `auto_link`/`auto_close` in both text and JSON output. An inert watcher previously logged only the project count, which a fully skipped set looks identical to. A config that will not load costs `tk watch status` the project list and nothing else — the error is reported as a line (`config_error` in JSON) beside the running state, which the pid file answers on its own.
- Concurrent writes to one ticket no longer lose updates. Every mutating path was an unguarded read-modify-write over a whole ticket file ending in `os.WriteFile`, so overlapping writers discarded one another's work: 20 concurrent `tk add-note` processes against one ticket left 10 to 15 of the 20 notes, and most of the losers exited 0, so nothing told the caller. Three things now stand between a writer and that outcome. A per-ticket advisory lock (`flock`, in `pkg/ticket/lock.go`) serialises writers to one ticket — and only to that one, so unrelated tickets stay uncontended; the lock files sit under the user's cache directory (`tk/locks`) rather than in the store, because `tk sync` stages `tickets/` wholesale and would ship them to every other machine, and because a per-user directory is owned by its user by construction where a shared `/tmp` is not. They stay under `HOME` and are not moved by `TK_STORE_ROOT`: a lock is about the processes on this machine, keyed on the store directory, not about the store's contents. `FileStore.writeTicket` replaces the ticket file atomically, writing a temp file in the same directory and renaming it into place, so no reader can observe the zero-length window `os.WriteFile`'s truncate-then-write opened. And `FileStore.Update` compares the version the ticket was read at against the file's current bytes, refusing with the new exported `ticket.ErrConflict` rather than overwriting a change it never saw. A ticket built by its caller rather than read from the store carries no version and is written unconditionally, which is where the compare-and-swap deliberately does not apply. Scope is one machine: a store shared between machines is exchanged by git commits, and `flock` over a network filesystem is unreliable.
- The accumulating writes — `tk add-note`, `tk dep`/`undep`, `tk link`/`unlink`, `tk verify`, the journal watcher's auto-close, and the `ticket_add_note`, `ticket_dep`, `ticket_link` and `ticket_verify` MCP tools — go through the new `ticket.Mutate`, which holds the ticket's lock across the read as well as the write. They append to what the ticket already holds, so a conflict error would leave the caller nothing to do but read and apply the change again. Everything else (the TUI, `tk edit`'s epic cascade, `tk move`) keeps the plain `Update` and now fails loudly on a conflict instead of clobbering. `tk link` and `tk unlink` write the two sides of a link one lock at a time — holding both would deadlock two runs naming the same pair in opposite orders, and the pair was never written atomically before this either.
- Ticket file modes survive the move to an atomic write. A rename publishes whatever mode the temp file carries, where `os.WriteFile` left an existing file's mode alone and masked a new file's `0644` through the process umask — so the replace reproduces both: an existing ticket keeps its own mode (a store under `umask 077` is no longer widened to world-readable by the next note), and a new one is created `0644` masked by the umask, read once at package initialization. A ticket file whose mode denies the current user write is refused rather than replaced, since renaming over a file consults the directory's mode and not the target's; without that, a ticket someone deliberately made read-only would be silently overwritten.
- `Delete` takes the ticket's lock. An unlocked delete could land between a concurrent write's read and its rename, and the rename then recreated the ticket the delete had removed.
- A failed mutation-log append no longer corrupts the TUI. The warning was written straight to stderr from inside `pkg/ticket`, and `tk ui` runs bubbletea in the alt screen with nothing redirecting that fd, so the message painted over the rendered frame instead of reaching the user. It now goes through `ticket.Warnf`, a replaceable package-level sink that defaults to stderr, and `tk ui` points it at a warning row of its own for the life of the program — flattened and clamped to one line, rendered as the last row of the frame so it survives both the success message every mutation emits right after the write and the overlay that write usually leaves open. The sink sends the message from a goroutine of its own, since a TUI write runs inside `Update`, on the very goroutine the program's message channel is drained by. The CLI and `tk serve` are unchanged: the warning still lands on stderr, which for the MCP server is its log rather than the protocol stream, and a broken log still never fails the mutation itself.

### Removed
- **Breaking:** the local `.tickets/` store, and with it the `TICKETS_DIR` environment variable. tk resolved two store topologies and branched on which one it had at every resolution site, but it has not created a local store in a long time: `tk init` writes `store: central` unconditionally and copies any `.tickets/` it finds into the central store, and every registered project on every machine we have is central. Store resolution is now one path — the repo (`--repo` when given, else the working directory) resolves to the project it owns in `<central_root>/tickets/<project>/`, and nothing else — so `FindTicketsDir` and its five callers are gone, along with the `.tickets/` walk-up, the `".tickets"` fallback and the local `--repo` probe in the CLI's resolution, `ResolveStoreForRepo`'s local fallback (which is `tk move`'s destination resolver, so a `.tickets/` is no longer a move target), the `entry.Path` store `tk serve` and `tk watch` built for a non-central project, and `ticket_create`'s `.tickets/` search for its `repo` argument. **Nothing on disk is deleted or rewritten:** a repo that still holds a `.tickets/` gets an error naming that directory and pointing at `tk init`, which copies the tickets into the central store and leaves the original in place as a backup. A repo that owns no central project is likewise an error naming it — it no longer falls back to a relative `.tickets` that does not exist, which turned "this repo is not registered" into an empty ticket list. `TICKETS_DIR` goes because it was the last non-central entry point: undocumented, absent from the README, and it built a store with an empty project name — the exact shape this removal exists to eliminate. `TK_STORE_ROOT` is the supported way to point tk at a throwaway store, and it moves the whole store rather than one directory.

### Changed
- The per-project `store` config field is kept for reading, and `central` is the only value that means anything. It is read through one predicate (`CentralRegistered`) and no code path branches on it having another value: a shared config still carrying `store: local` reads as an unregistered project, so the repo owns no store and every surface says so by name rather than resolving something tk cannot read. The field is neither dropped from the format nor rewritten, so old configs load and a save round-trips whatever a project entry already carried instead of editing another machine's registration. One behaviour change follows from it: the commit-journal watcher (`tk watch`, and the loop inside `tk serve`) builds a store only for a centrally registered project, so a `store: local` entry with `auto_close` set no longer has its commits auto-close tickets in a `.tickets/` beside the repo — those commits are still journalled.
- `tk audit`'s central-store scoping stops being a limitation: it spans every project directory in the central store, which is now every store tk can resolve. It still requires a configured central store — without one there is nothing to audit at all.
- MCP `ticket_create`'s `repo` argument resolves through the same function the CLI's own store and `tk move`'s destination do, where it had a resolution of its own that disagreed with them twice. A repo owning an *unregistered* central project — a directory under `<central_root>/tickets/` with no `store: central` entry in config — was reported as having no ticket store at all, while `tk --repo <path> ls` listed that project's tickets: one repo, two answers. It now resolves, and the response carries an `unregistered_warning` field with the same sentence the CLI prints on stderr, since `MultiStore.Create` refuses writes to such a project and stderr here is the server's log rather than anything the calling agent reads. The shared resolution also bounds the project name before joining it into a store path: names come from the shared `config.yaml`'s map keys, which arrive over git from other machines, and `filepath.Join` cleans a `../` rather than failing — so a crafted key could steer a `ticket_create` write outside the central tickets root. A repo owning no project, and one still holding a legacy `.tickets/`, are reported exactly as before.
- A repo resolving to no store is now told about a legacy `.tickets/` at its git top level, not only one in the directory the command ran in — running `tk ls` from a subdirectory previously got the plain "no ticket store found" with no mention of where those tickets went, which is the case most easily mistaken for data loss. The message names the directory to run `tk init` in, since the two can differ. Still two stats and no walk up the tree: the git top level is the same root `tk init` migrates, where a walk from anywhere under `$HOME` would reach a central root named `~/.tickets` and report the central store itself as a stale local one.
- A `~/.ticket/config.yaml` or shared `<central_root>/config.yaml` that fails to load is now reported as itself (`load ticket config: ...`) by every command's store resolution, where only `tk move` did that and the rest reported the repo as owning no store — sending the user to `tk init`, which does not fix a half-written or conflicted config. The shared config is the one that arrives over git from other machines, so it is the one that fails this way.
- `tk ui` spawns a work session in the repo it resolved the store from when the project's config records no `path` — a project registered on another machine has none locally. It previously used the parent of the tickets directory, which under the central store is `<central_root>/tickets`: a directory inside the ticket store, and the one `spawn_command` would have run `/work` in.
- A default `tk ls` hides `done` as well as `closed`, so the listing is the live board rather than a growing archive with the `open`, `ready` and `backlog` rows buried in it — done tickets accumulate without bound in a long-lived project and nothing was dropping them. The terminal rows are still reachable by name with `--status done` / `--status closed`, and the new `--all` flag turns the exclusion off: `--status` shows one status at a time, so `--all` is the only listing of the whole board. Scoped to the CLI's `tk ls` — MCP `ticket_list`, `tk query` and the TUI are unchanged. Since an epic reads the status its children imply, a finished epic drops out of the default view along with its children, and an epic demoted back to non-terminal by a file that cannot be read reappears with them.

## [7.8.0] - 2026-08-11

### Added
- `TK_STORE_ROOT`: an environment variable that points tk at an explicit store root, overriding the configured `central_root` so a test harness or an agent can run `tk serve` against a throwaway store. There was no such lever before — cwd, `--repo` and `TICKETS_DIR` all still resolved the real central store out of `~/.ticket/config.yaml`, so an integration test of an MCP client wrote into the shared store and the background sync committed and pushed it. With the override set, the store is `<root>/tickets/<project>/`, the shared config `<root>/config.yaml`, and the local config `<root>/.ticket/config.yaml`: nothing reads or writes the configured store tree, `<central_root>/config.yaml` or `~/.ticket/config.yaml`, and the only store path `$HOME` still decides is where the commit journal lives, which is why the commands that touch it refuse to run at all (below). It is applied in the four config-resolution functions every CLI, MCP, TUI and library call site funnels through, rather than as a cobra flag, because `pkg/ticket` and `internal/mcp` resolve config directly and never see cobra's flags. `tk serve` starts on the override alone — no `~/.ticket/config.yaml` and no `central_root` required — and runs neither the sync loop nor the commit-journal watch, both of which write to the store; sync previously happened not to start against a temp root only because `findGitRoot` fails outside a git tree, which a temp dir created inside one would not. `tk sync`, `tk watch` and `tk recompute` refuse to run under the override for the same reason, so the guarantee holds for the whole CLI rather than for `tk serve` alone: sync resolves the store root and commits and pushes whatever git repo encloses it; watch keeps its pid file, log and journal state under `$HOME` — which the override does not move — so it would auto-close sandbox tickets while journalling into the real home; and recompute deletes `~/.ticket/state/<project>/commits.jsonl` and rebuilds it from the project its config names, so a sandbox registering a project name the machine also has would destroy the machine owner's journal for it. `tk init` is the one write command that keeps working under the override — a harness needs it to register the sandbox project, since a central write to an unregistered project is refused — but it no longer bootstraps git in the store root: a throwaway store keeps no history, and an override root nested inside another repo would commit the sandbox's own store files into that repo, which the override's "nothing is committed or pushed" contract forbids. `verify_allow` and `spawn_command` are the settings the override does not move: both are always read from `~/.ticket/config.yaml`, because each decides code that runs as the machine's owner — which bounds what a store root can supply, not a caller who also controls the environment, since whoever can set `TK_STORE_ROOT` can generally set `HOME` too. A root the caller names could otherwise widen the allow-list (`verify_allow: [sh]`) or, having no config at all, restore the built-in defaults over a list the owner had narrowed; and `spawn_command` is itself the string the TUI hands to `sh -c` when `w` spawns a work session, so a sandbox supplying one would run its own code the moment someone pressed the key. A value that is not an absolute path is an error naming the variable on any command, raised before any store is resolved rather than falling through to the nearest `.tickets/`, since a silent fall-back is the failure this prevents; the empty string is such a value and not an absent variable, so `TK_STORE_ROOT=` — or a harness exporting a path that expanded to nothing — is refused rather than quietly resolving the real store. The guarantee covers tk's own store resolution and nothing wider: `ticket_create`'s `repo` argument resolves a caller-supplied absolute path before any config lookup, and a verify command that `verify_allow` permits (`go run pkg@version`, `make -f <file>`) runs code from outside the repo — both are separate controls.
- TUI list view: an `EPIC` column between `PRI` and `TYPE` on the inbox, all, and done tabs, showing the 4-char suffix of the ticket's epic (an em-dash when it has none), so a row's epic is visible without opening it. The column sorts like any other via `s`/`S`; tickets with no epic sort last ascending. A `parent` that names no epic in the store — `tk delete` on an epic leaves its children pointing at nothing — reads as epic-less everywhere: em-dash in the column, and the row stays visible on the backlog tab with its own tab count instead of rolling up under an epic the epics tab no longer shows. An epic always keeps its own backlog rollup row, including the sub-epics a store written before the one-level rule can still hold, so its children never roll up under a row that is not drawn.
- `tk audit [--project=NAME]`: report tickets whose `parent` breaks the one-level epic hierarchy — a parent that is not an epic, a parent that does not resolve, a parent in another project, an epic that has a parent, or a parent cycle. It spans the central store, takes `--project` to scope to one project like `tk frontier`, and supports `--json`. Each parent is resolved inside the project that owns the ticket, the same way a write resolves it, so the report and the write path agree on every ticket. A project the audit could not read is named as a warning (and carried in the JSON as `skipped`) rather than dropped, so "No parent violations." never speaks for a store that was only partly read. Read-only: it names what to fix and never rewrites a ticket, so a store can be cleaned before a write trips on it. A second section reports every epic whose stored `status` differs from the one it now derives from its children (JSON `epic_status`), read in the same per-project pass and under the same `--project` scoping: statuses were derived without migrating what was stored, and the derivation happens at the read choke point, so this is the only place the two can still be compared. An epic whose file stores `closed` with no `abandoned` flag is classed `stored-closed` and called out separately — either a hand-close from before the change or a derived `closed` a write carried into the file, which the file cannot tell apart, and `tk edit <id> --status closed` re-records the ones that were real — where the rest are `stale-status`, the drift between a stored value and the children that deriving the status removes. A stored status the store does not recognise is printed quoted: it is read straight off a file another machine may have written.
- TUI edit form: a `Parent` field, between `Status` and `Note`, prefilled with the ticket's parent. Typing an epic repoints it and leaving it empty clears it, validated by the same write the CLI and MCP go through, so the remedy the one-level hierarchy's rejection names is performable in the TUI instead of only naming a field the TUI could not touch.

### Changed
- A ticket's `verify:` command no longer runs through a shell, and runs at all only if its program is on a machine-local allow-list. `tk verify` and MCP `ticket_verify` took the command string verbatim out of a ticket's acceptance criteria and handed it to `sh -c` in the project's repo directory with the full environment — so a bullet reading `verify: rm -rf ~` did exactly that to whoever ran verify on the ticket. Ticket bodies are written by agents and replicate to every machine over the shared store's git remote, which made this a prompt-injection-to-code-execution path with no exotic steps. The command is now split into arguments on whitespace — single and double quotes group an argument, so `-run 'TestA|TestB'` still works — and exec'd directly: nothing is expanded, so `;`, `|`, `&&`, `$(...)`, backticks, globs and `~` are literal characters passed to the program rather than shell syntax, and the second half of a chained command never runs. The program must then exactly match (not by basename — an entry of `go` does not admit `/tmp/evil/go`) an entry in `verify_allow` in `~/.ticket/config.yaml`; anything else is reported as `refused` without running, naming the command and how to permit it. `verify_allow` is read from that machine-local file alone: an entry in the shared `<central_root>/config.yaml` is ignored, because that file syncs over the same remote that would carry a hostile command, and there is no flag, MCP argument or ticket field that grants permission — an agent can run what the machine's owner has already allowed and can authorize nothing further. The control fails closed in both directions: an explicitly empty `verify_allow: []` refuses everything, and a local config that cannot be parsed — half-written, conflicted, unreadable — also refuses everything and says so in the refusal, rather than silently restoring the defaults over a list the user had narrowed. Unset, the list defaults to `go`, `make`, `cargo`, `pytest`. Listing a program is not a claim that the program is safe: it trusts whoever can write a verify line with everything that program can do, including the forms that run code from outside the repo — `go run pkg@version` and `go install pkg@version` fetch and execute a remote module, `cargo install <crate>` fetches a remote crate and executes its `build.rs`, and `make -f <file>` or `make -C <dir>` runs recipes from a file the command line names rather than the repo's Makefile. They are in the default because `go test` is the primary use case and cannot be dropped over them; what the default buys is that a verify line cannot name an arbitrary program, not that the listed ones stay inside the repo. Shells and interpreters are absent because they remove even that while buying nothing back — `sh -c` or `python3 -c` runs whatever string the ticket supplied — and `swift` is absent on that rule rather than as a build driver, since `swift -e '<code>'` runs arbitrary Swift with the full standard library and needs no remote module to do it; a Swift project gets `swift test` back by adding `swift` to `verify_allow` itself, accepting that a verify line can then run any Swift the ticket names. `npm`, `pnpm` and `yarn` are absent because `npm exec`, `pnpm dlx` and `yarn dlx` fetch and run an arbitrary registry package as a documented feature; a JS project adding one back is opting into a verify line being able to run any published package. `refused` is a fourth status alongside pass/fail/unverified, counted separately in the summary, the `--json`/MCP report (`summary.refused`, and `ok` is false) and the recorded Test Results — a criterion that never ran is not one that disagreed. Control characters — C0/C1, DEL and the Unicode format characters, bidi overrides among them — are stripped from a criterion's text as well as its command before either is printed or recorded, so an escape planted anywhere in the acceptance criteria can neither repaint the terminal the verdict is being judged on nor replay out of the recorded Test Results on every later read; tab is exempt, carrying no repaint risk and being ordinary in a hand-written bullet, while newline and carriage return are not, because the record is one line per criterion. A command's own output is left raw so a test runner's colours survive. `ticket.RunVerify` (exported library API) takes two new parameters, the allow-list and the error that made it unreadable, so an external caller must supply them — pass `project.VerifyAllow()`'s two results to get the CLI's behaviour. Commands that were already shell syntax rather than a plain invocation need rewriting: quote the shell fragment and name the shell (`/bin/sh -c 'a; b'`) with that shell added to `verify_allow`. No ticket in the store carried a verify command when this shipped, so nothing was silently disabled.
- An epic's status is now derived from its children instead of stored: no children reads `backlog`, any child open reads `open`, every child done reads `done`, every child terminal with at least one closed reads `closed`, and anything else reads `backlog`. `done` and `closed` split the terminal case because they say different things — `done` means the work completed, `closed` means it did not — so an epic whose children were every one abandoned or moved to another repo reads `closed` rather than claiming to have finished. An epic never reads `ready` — `ready` means "available to pick up" and an epic is not picked up directly, so epics no longer appear in `tk frontier` or the frontier half of `ticket_ready`. Derivation happens at the store read choke point (`FileStore.List`/`Get`, which `MultiStore` delegates to), so `tk ls`, `tk show`, `tk query`, the TUI tabs, and every MCP response agree without a display site of its own. An epic's `completed` date is derived with it — the date its last child reached a terminal state, blank while the epic is not terminal — where before nothing wrote an epic when its last child finished, leaving the COMPLETED and DURATION columns empty on a finished epic and a stale date beside one a reopened child had made live again; an epic's file now stores no `completed` at all, so no date the writer's clock produced can surface beside one. The one thing about an epic still asserted rather than derived is the intent to abandon it, and it lives in a new `abandoned: true` frontmatter field rather than in `status`: setting an epic's status to `closed` through `tk edit`, the TUI or MCP `ticket_edit` records the flag and closes every non-terminal child in the same action (children that already finished keep their `done`), and setting any other status takes the intent back. The children the abandon closed are reported with the edit — named on `tk edit`'s and the TUI's status line, returned by MCP `ticket_edit` as `closed_children` — so a write that mutated other tickets says so on success as well as in the failure case, where the error names which children were closed and which were not rather than reporting success. Changing a ticket's type to `epic` is judged the same way as any other edit: `tk edit <id> --type epic` on its own carries the status the ticket was read with and is not read as a decision, while a status set in the same call is a status set on the epic that edit makes — `closed` abandons it, and anything else is refused. Reopening a child of an abandoned epic un-closes the epic — the flag is honoured only while every child is terminal — and finishing the child brings it back. Any other status set by hand on an epic that is not abandoned is refused, naming the epic and what it currently reads. Only a status a writer actually set counts as either decision — `tk edit --status`, MCP `ticket_edit`'s `status` argument, or the TUI form's status field cycled off the value it was opened with — and each edit path carries that signal into the store rather than the store guessing at it from the value: a status that merely rode along with an edit to some other field is not judged at all, so it can neither record an abandon nor be refused for disagreeing with children that moved between the read and the write. Because the intent is a stored flag rather than a value read off the children, an edit to any other field of an epic can neither drop it nor invent one: an epic's `status:` field is now advisory, never consulted by the derivation, so what a read-modify-write carries back is inert wherever it lands. Writes tk makes on its own behalf express no intent either: `tk move` closes an epic in the source repo to record that it left, which is not an abandon (a non-recursive move leaves the children staying behind exactly as they were, and the moved-away epic reads as whatever children stayed with it), and the commit-journal watcher no longer auto-closes an epic at all — a commit carrying `Closes: [<epic-id>]` or `Fixes: [<epic-id>]` is reported as a skipped warning and excluded from the closed count, since writing the epic would change nothing an epic's children do not already say. This replaces the epic-done guard and the parent-status propagation that kept the stored value in sync, both of which are gone: a value defended by a guard on every write path had already fallen out of sync twice. **Existing stores are not mutated and no stored status is migrated** — the derivation ignores it, so an epic that was closed by hand before this change reads as its children imply (`backlog` for a childless one) until it is closed again, which records the flag. Treating a legacy stored `closed` as the intent at read time was rejected deliberately: an epic that merely derives `closed` has that value written into its file by the next unrelated edit, and reading it back as an assertion would forge exactly the intent this design separates out. Re-run `tk edit <epic> --status closed` on any epic that should stay abandoned; every other epic whose displayed status moves was drift between the stored value and the children, which is what this removes. `tk audit` lists both — every epic reading a different status than its file stores, with the ones storing `closed` and no flag called out separately as `stored-closed` — so no epic's move has to be discovered. Re-record those before editing them: the stored value is the only surviving trace of the decision, and the next write of the epic replaces it with the derived one. Neither drift class is confined to the migration, and the report says so: every write of an epic stores the status it derived at that moment, which the next change to a child makes stale, and an epic that derived `closed` when it was written comes back as `stored-closed` — so a stored value is evidence of a decision only on a file older than this change.
- TUI: the epics tab is now a grouping over the shared column table rather than a parallel tree model, and one predicate decides tab membership for every tab. Each tab's show/hide rules were written out in three places — the dashboard's row builder, the epics view's own rules, and a hand-written mirror behind the tab-bar counts — and had drifted: the counts skipped the empty-status guard, so a ticket with no status inflated the `all` count above the rows it labelled, and ignored the type and text filters, so every count froze at its unfiltered total the moment a filter was typed. Counts now come from the same code path the rows do, so a tab bar always reports what its tab renders, filtered or not (on the epics tab it counts epics: children are nested inside a group that is already counted, so expanding one does not inflate it). Expand/collapse, jumping to an epic from a backlog rollup, opening a child, the per-epic progress bar, and the epics tab's AGE-descending default sort all survive the fold; search, type filter, and the row commands (`p`, `m`, `d`, `y`, `w`) now work on the epics tab like any other. The `EPIC` column is dropped from the backlog and epics tabs, where it could only ever render an em-dash or repeat the epic drawn directly above the row. The epics tab's "No epics found." becomes a shared "No tickets found." placeholder that every tab draws when nothing passes its rules and filters, instead of only the epics tab having an empty state.
- The ticket hierarchy is now one level deep and enforced on write: a ticket's `parent`, when set, must resolve to an epic in the same project, and an epic itself cannot have a parent (sub-epics are no longer representable). Enforcement lives at the store write choke point, so CLI, MCP, and TUI writes are all covered, and the error names the offending parent and its actual type. A parent in another project is refused by name rather than surfacing as a confusing "not found" — a per-project store cannot resolve one, and an epic and its children are meant to live together. Parents resolve through the same matching as everything else, so full, partial (`eb6c`), and namespaced (`project/epic-abcd`) forms all work, and the resolved epic's ID is what gets stored — a partial form is not kept verbatim, where it would name an epic the write accepted and no view could find. Existing stores are never mutated: tickets that break the rule still load, list, and render, but writing one back is refused until its `parent` is cleared or repointed — run `tk audit` to find them. This also gives "an epic's children" a single definition: the backlog tab's rollup count and the epics tab's expansion previously disagreed for nested epics (an epic transitively holding 17 tickets showed 0 on the backlog tab), and both now report the same direct children.
- MCP `ticket_edit` clears a `parent` when passed an empty string, where it previously read that as "no change". Clearing is half the remedy the validation error names, and it was the half an agent could not perform. Omitting the field still means "no change"; `tk edit --parent=''` was already able to clear it.

### Fixed
- `tk move` now refuses a destination that resolves to the store the ticket already lives in, naming the project and saying no move was performed, where it previously ran the move and reported success: the original was closed with a "Moved to ..." note, a copy was created under a **new ID**, and every `parent`, `deps` and `links` reference among the moving set was remapped to it — so the ticket went nowhere, lost the ID other tickets and commit messages reference, and left that ID closed. It was reachable by typing a relative target from inside a registered repo (`tk move <id> to` from the `from` repo resolves `to` to `$PWD/to`, which sits under the path registered for `from` and prefix-matches back to it), but the refusal does not depend on how the target is spelled: the check compares the resolved store directories, not the strings. Directory identity rather than project name, because a repo's own `.tickets/` carries no project and comparing names would read two unrelated local stores as one; identity is the inode where both directories exist, so every spelling of one directory is caught — a symlink, a bind mount, a hard-linked parent, and the case variant a canonicalized path string misses, since resolving symlinks does not fold case and APFS is case-insensitive by default, which a repo-owned store reached as `../proj` from a differently-cased path hits. Comparing the canonicalized paths remains the fallback for a directory that cannot be stat'd: a registered central project that has never held a ticket has no directory until the move makes one. The refusal lands in `MoveTicket` before anything is read or written, so a recursive move is refused as a whole — the move is not atomic, and one discovered partway would leave some descendants moved and some not — and so `tk move` and the TUI's move picker, which both go through it, refuse identically, the picker saying so on its status line. Cross-project moves are unaffected. Re-parenting a ticket within its project is `tk edit --parent`; nothing else about a ticket changes on a same-store move except the ID it loses. The resolution defect behind the relative-path case is separate and unchanged here.
- `tk show` now renders a dep or link stored in namespaced form (`project/ticket-id`) with the target's real status and title, where it printed `[unknown]` with neither. The relationship sections matched a reference by exact string against a map keyed on the IDs `List()` returns, which a per-project store returns bare — so a reference written through MCP, which namespaces every ID it hands out, never matched, while `tk dep` canonicalizes to bare when writing into the same project directory. A project store therefore holds both forms and only one of them rendered, in a file whose parent and `## Children` lines were already namespace-tolerant: within one `tk show` the parent resolved and the blockers did not. `tk move` writing its remapped references namespaced (7.8.0, above) made every moved ticket hit it, but the defect predates that and is already sitting in stores. `## Blocking` had the same defect presenting as under-reporting rather than as `[unknown]`: it tested another ticket's dep against this one's ID by exact string, so a ticket depending on this one namespaced was silently omitted from the section entirely. Both now resolve through the index `pkg/ticket` already uses for dep and parent lookup, which matches exactly first — so a bare reference resolves exactly as before — then falls back to the bare half. The fallback keeps that index's two guards, so it cannot make a dangling reference look resolved: a bare half two listed tickets share is refused as ambiguous rather than guessed between them, and a prefix naming another project matches nothing, the rule `FileStore.Resolve` already applies to every cross-project reference. A reference that genuinely names no ticket still renders `[unknown]`. Display only — nothing about what `tk move`, `tk dep` or MCP write is changed.
- `tk sync` no longer commits and pushes the enclosing repository's work when the central store root sits inside another git repo. Two defects combined on the path that actually pushes, and that `tk serve` runs unattended on a background loop. First, the commit was unbounded: staging was scoped to `tickets/` and `config.yaml`, but the is-there-anything-to-commit check and the `git commit` itself carried no pathspec, and a pathspec-less commit takes the whole index — so whatever the enclosing repo's owner had staged went out under `tk: sync tickets`, and because the emptiness check saw those entries too, a cycle with no ticket changes at all still produced and pushed a commit made entirely of that work. Second, sync ran git in the wrong directory: it passed `findGitRoot`'s answer, which is `rev-parse --show-toplevel` and therefore the *enclosing* repo's toplevel for a nested store, so the scoped `git add` staged `<enclosingRoot>/tickets/` and `<enclosingRoot>/config.yaml` — paths that are not the store's and may not exist. Together they produced a pushed `tk: sync tickets` commit containing none of the user's tickets and all of their unrelated staged work. Sync now runs every git command with `-C <storeRoot>` and scopes staging, the emptiness check and the commit to the same relative paths, matching `tk init`'s bootstrap exactly; nesting is not refused, because init deliberately supports that topology (it detects the enclosing repo and skips `git init`, since the parent repo owns history) and refusing would silently stop syncing stores set up that way on purpose. `findGitRoot` is still called at both call sites, but only as the gate that produces "central store is not in a git repository" — its result is no longer used as a working directory. A pathspec'd commit leaves the rest of the index alone, so work staged in the enclosing repo stays staged; the same scoping is applied to the `git reset` that unstages after a conflict-marker refusal, which previously unstaged the enclosing repo's work along with tk's. Three further consequences of the wrong directory are fixed with it: the `.tk-sync-blocked` marker, a machine-local tk artifact that is never staged, now lands in the store root instead of the enclosing repo's root, and `tk status` reads it from there rather than from the toplevel `findGitRoot` resolves — otherwise a nested store reported sync `ok` while sync was blocked, and a marker a pre-upgrade tk left at the old location reported blocked forever, on the only user-visible signal that unattended syncing has stopped; the mid-rebase check resolved `<gitDir>/.git/rebase-merge`, a path that cannot exist for a nested store, so it reported every rebase as finished and let sync commit mid-rebase — it now asks git for the real git dir via `rev-parse --absolute-git-dir` and stays blocked when git cannot answer; and the unmerged-paths and staged-conflict-marker scans, which relied on cwd defaults, are now scoped to the same two paths, so a conflict elsewhere in an enclosing repo — which sync neither created nor can resolve — no longer blocks the store's own syncing. Scoping those guards narrows what they can see, so a pre-flight repo-state check now runs before anything touches the index, on every cycle rather than only when a marker is already on disk: sync resolves `rev-parse --absolute-git-dir` and refuses the cycle — writing the blocked marker with the state named — while `rebase-merge`, `rebase-apply`, `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD` or the `sequencer` directory is present, or while git cannot say where its git dir is. The last two matter for a reason the merge case does not share: git's own partial-commit refusal keys only on `MERGE_HEAD`, `CHERRY_PICK_HEAD` and the rebase directories, so a conflicted `git revert` in the enclosing repo leaves unmerged paths outside `tickets/` and `config.yaml` — invisible to the scoped unmerged-paths check — and a `git commit -- <paths>` that git accepts, so sync committed onto the mid-revert index with nothing objecting anywhere. `sequencer` backs the multi-commit forms of both cherry-pick and revert and is named as either, since telling them apart means parsing `sequencer/todo`. Without it, a nested store whose owner had an interactive rebase paused at `edit` or `break` passed every scoped guard (the pause leaves no unmerged paths inside the store, and `@{u}` does not resolve on a detached HEAD, so the pull returned early), committed onto the rebase's detached HEAD, failed to push from it, and then ran `git rebase --abort` on a rebase it had not started — discarding the owner's in-progress rebase and every commit they had made during it. The same check subsumes the mid-merge case, where sync previously returned git's raw "cannot do a partial commit during a merge" every tick with no marker and no resolvable reason. Sync also no longer aborts a rebase it did not start, at either retry site: it records whether one was already underway before running its own `pull --rebase`, and counts a git dir it cannot resolve as one, since an abort that should not have run destroys work while a skipped one only leaves a rebase to finish. What is scoped and what is not: only the index-touching commands carry the pathspec — staging, the staged-changes checks, the commit and the reset after a refusal — so sync cannot commit or unstage anything outside the store, but `git push` and `git pull --rebase --autostash` are repo-wide and branch-wide and cannot be scoped, since `-C` only sets the working directory. So in the routine nested case where another machine pushed tickets first and the local push is rejected, tk stashes the enclosing repo owner's *entire* uncommitted worktree, rebases *their* current branch onto its upstream and pops the stash, from `tk serve`'s background loop; and a stash-pop conflict outside `tickets/` and `config.yaml` is deliberately invisible to sync's guards, which are scoped so that a conflict sync neither created nor can resolve does not stop the store syncing. Gating the remote half on the store owning the repo was rejected: refusing to push a nested store is the same silently-stop-syncing failure this change exists to avoid. That unscoped remote half is left as it stands and tracked as `ticket/tk-sync-runs-51d4`. Two narrower fixes ride along. The staged-conflict-marker scan no longer depends on git config, running `-c core.quotePath=false diff --cached --name-only --no-relative -z` (git 2.28+): `core.quotePath` defaults to *true* and octal-escapes and quotes a non-ASCII name, and `slugifyTitle` keeps Unicode letters so tk writes such names itself (`ticket-über-größe-7cd2`), while a user-set `diff.relative` reports store-relative names instead of toplevel-relative ones — either way the follow-up `git show :<name>` missed and the file was skipped, so a staged ticket carrying conflict markers, content that arrived over git from another machine, was committed and pushed. That scan now blocks when it cannot run, rather than reporting no markers: `--no-relative` needs git 2.28+, so on an older git the guard would have disappeared silently every cycle with nothing in the log, defeating the one thing it exists for — the same unknown-is-not-none split the repo-state check already made. And a failing `git add` is now surfaced as a warning naming the path and git's message instead of being discarded: an enclosing `.gitignore` covering the store root makes `git add -- tickets/` exit non-zero — the case `tk init` now fails loudly on — after which sync found nothing staged and returned the no-op every cycle forever with nothing in the log. Both that and a `stat` that fails for any reason other than the path being absent write the blocked marker, like every other terminal refusal on this path, so `tk status` reports it: a gitignored store root is permanent rather than transient, and without a marker status read `ok` forever while every cycle failed. A path that is merely absent is still skipped, as in init's bootstrap: `git add` on a pathspec matching nothing is a hard error, and a store with no tickets yet, or whose shared config was deleted, has nothing there. Finally, the ticket filenames these warnings name are sanitized before they are interpolated — C0 and C1 control characters, DEL and the Unicode Cf format characters replaced with U+FFFD, TAB exempt, the same rule `tk verify` applies to a criterion, now shared as `ticket.SanitizeControl` rather than copied. git permits newlines and control characters in a path and `-z` hands the name over raw, so a ticket file pushed from another machine could forge lines in `tk status`'s output and in `tk serve`'s log. Display and log forging only: the marker is not one of the paths sync stages, so nothing forged is ever committed or pushed, and none of it reaches a shell.
- `tk move <id> <repo-path>` now resolves its target to the project that repo owns in the central store, else to a `.tickets/` the repo owns, where it previously joined `.tickets` onto the path unconditionally. Every project registered by `tk init` has been `store: central` for a long time, so moving into one either failed outright with "target tickets directory does not exist", or, when the destination repo happened to still hold a stale `.tickets/`, wrote the ticket into a directory neither `tk ls`, `tk ui` nor the MCP server would ever read — while the source was closed with a "moved" note, so an orphaned ticket read as a relocated one. The moved ticket now gets its new ID in the destination project's namespace, and the remapped `parent`, `deps` and `links` carry that prefix, which is the form a central project's tickets reference each other in. A repo that resolves to no store is refused by name (`no ticket store found for <repo>`) instead of having one created for it; a registered project that has never held a ticket still gets its directory made on the way in, the same as `tk create` does. The TUI's move picker resolves the same way through the same function, and now lists the repos beside the project's own repo rather than the directories beside its central ticket store — under a central store the picker's candidate list had been empty. The provenance notes on both sides name the destination project rather than the path above the store directory, which said nothing about where the work went. A target that resolves to an *unregistered* central project — a directory under `<central_root>/tickets/` with no `store: central` entry in config — is now named in a warning before the move, the same sentence `tk ls` prints on stderr and the TUI carries on its status line, where a stderr write would corrupt the alt screen: `MultiStore.Create` refuses writes to such a project, so a move into one was the quietest way to put a ticket where nothing else will write. A `~/.ticket/config.yaml` that fails to load is now reported as itself (`load ticket config: ...`) instead of as a repo with no registration: a half-written or unreadable config resolved nothing and sent the user to `tk init`, which does not fix it — on the one command whose resolution feeds a write into another repo's store. It does not fall through to a `.tickets/` in that case either, since the config is what decides whether that repo's tickets are central, and a local directory found without it may be the stale one this change exists to avoid writing to.
- A ticket ID or title carrying shell syntax can no longer run commands when the TUI spawns a work session with `w`. The `spawn_command` template is interpolated and handed to `sh -c`, and a ticket ID is a filename in the central store, which other machines push to over git — so a pushed file named `x'; rm -rf ~; echo '.md` closed the default template's `osascript -e '...'` quoting and ran in that shell the moment someone pressed `w` on the row, while `$(...)` and backticks passed the outer single quotes literally and then expanded in the interactive shell the default types its payload into via iTerm's `write text`. The two placeholders are now handled by their own kind: `{id}` must be letters, digits, `.`, `-` or `_`, with at most one `/` for the project namespace, both halves non-empty and each starting with a letter or digit — anything else refuses the spawn with a status line naming the ID. That shape covers everything `GenerateID` emits (letters, digits and `-`, always opening with one) plus the `.` and `_` of hand-named IDs already sitting in stores, such as `ghostwheel/g-101.2`; the leading-character rule keeps `..` and `../x` from being handed to `/work` as a ticket to pick up, and keeps a pushed `-rf` or `--flag` out of a custom template that interpolates `{id}` in argument position, where it would reach the program as an option token rather than a ticket — quote `{id}` in a custom template, exactly as you must `{title}` and `{dir}`. `{title}`, being free text where an apostrophe is ordinary, is sanitized rather than refused, with `$`, backtick and `!` joining the quotes and backslashes that `{wtitle}` already replaced with spaces (`{wtitle}` gains them too — it was interpolated into the same payload); `!` is there because the interactive shell the default template types its payload into runs history expansion before parsing and double quotes do not suppress it, so a title like `Fix !git` either aborted the whole line — a spawn that silently did nothing — or spliced history text in to be re-parsed. Both placeholders' sanitizing now covers control characters the way `tk verify` does — C0, DEL, C1 and the Unicode Cf format characters, bidi overrides among them — where it previously stopped at C0, so a title cannot repaint the terminal it is displayed on or render as something other than the command that runs. TAB is not exempt here as it is in a verify criterion: a window title has no tab stops to align. ZWJ and ZWNJ go with the rest of Cf, matching the same tradeoff. Sanitizing removes what breaks a *quoting layer*, not everything that is shell syntax — `;`, `&` and `|` survive — so a custom template must still quote `{title}` where it uses it, exactly as it must `{dir}`; the default template uses only `{wtitle}`, inside double quotes. Only the spawn is refused: a ticket with an out-of-shape ID still lists, opens, and edits everywhere else, and validating at load time was rejected deliberately, since a ticket that fails to load is a ticket that silently vanishes. `{dir}` and `{project}` are unchanged, coming from machine-local config rather than the shared remote. A custom `spawn_command` keeps working as written; what changed is what can reach it, so a template relying on a `$` or backtick arriving from a ticket's title needs another source. Every ID `tk` generates is accepted, including the non-Latin ones: `slugifyTitle` keeps any Unicode letter, so "Ticket über Größe" slugs to `ticket-über-größe-7cd2`, and the ID rule tests letters and digits by Unicode category rather than an ASCII range so it does not refuse what `tk` itself writes. That costs nothing, since every shell and AppleScript metacharacter is ASCII punctuation, and control characters and the invisible Cf format characters (U+202E and the bidi isolates) are not letters either way.
- `tk init` no longer commits the enclosing repository's worktree when the central store root sits inside another git repo. The bootstrap staged with a pathspec-less `git add -A`; since git 2.0 that stages the whole worktree of the repository that owns the path, and `-C` sets the working directory without scoping the pathspec — so init detected the enclosing repo, correctly skipped `git init` because the parent repo owns history, and then swept that repo's unrelated in-progress edits and untracked files (a `.env` among them) into a commit titled `tk: init central store`. Staging, the is-there-anything-to-commit check and the commit itself are now all scoped to `tickets/` and `config.yaml`, the same two paths `tk sync` has always staged, so init commits exactly what sync would; nothing else a store root holds was ever being synced. Scoping the commit matters on its own: a pathspec-less `git commit` takes the whole index, so work the enclosing repo's owner had staged and not yet committed went in under tk's message — and because the emptiness check saw those entries too, init could produce a commit consisting solely of that work and nothing of its own. A pathspec'd commit leaves the index alone, so anything staged in the enclosing repo stays staged exactly as its owner left it. A missing `config.yaml` — a central root whose shared config was deleted — is skipped rather than failing the whole bootstrap, since `git add` on a pathspec that matches nothing is a hard error, while a genuine `git add` failure, or a `stat` failing for any reason other than the path being absent, still returns an error naming the path. One case now fails loudly where it used to succeed badly: if the enclosing repo's `.gitignore` covers the store root — `central_root` of `~/code/notes/tickets` under a `notes` repo that ignores `tickets/` — `git add -- tickets/` exits non-zero with "The following paths are ignored by one of your .gitignore files" and `tk init` aborts before registering the project, where `add -A` skipped the ignored paths silently and reported success on a store whose contents the enclosing repo would never keep. Unignore the store root or move it out of that repo; the add is deliberately not forced. A second case fails loudly for the same reason: a pathspec'd `git commit` is a partial commit, which git refuses outright while a merge is in progress ("fatal: cannot do a partial commit during a merge"), so `tk init` against a store nested inside a repo that is mid-merge now aborts before registering the project. That is the right outcome — the old pathspec-less form staged the conflicted files along with everything else and *completed* the merge, conflict markers and all, under the message `tk: init central store`. Finish or abort the merge and re-run `tk init`. A rebase in progress is unaffected: git's refusal keys on `MERGE_HEAD`, which the sequencer does not set.

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
