# tk Specification

Version: 1.0
Date: 2026-02-19
Status: As-Is (captures current behavior exactly)

---

## 1. Purpose

tk is a local, file-based, git-native ticket management CLI designed for both human developers and AI coding agents. It stores tickets as Markdown files with YAML frontmatter in a project-local directory and provides commands for:

- Creating, editing, and deleting work items.
- Modeling directional dependencies, symmetric links, and parent-child hierarchies.
- Computing execution queues: what is ready, what is blocked, what is closed.
- Rendering ticket context with computed relationship sections.
- Emitting machine-readable JSONL for programmatic consumption.
- Reporting project health via analytics commands.

tk serves as the persistence layer for agentic development workflows. Agents create tickets to capture intent, document decisions and progress inline via notes, and commit ticket files alongside code changes. The tool's CLI surface is designed so that AI agents can learn it from `tk help` and operate it without special wrappers.

---

## 2. Design Tenets

1. **Plain text first.** Tickets are human-readable Markdown with YAML frontmatter. No binary formats, no databases.
2. **Local-first, git-native.** No server, no network. Ticket files live in the repo and version with code.
3. **Agent-compatible interfaces.** Stable CLI verbs, predictable output formats, and JSONL for automation. An agent should be able to learn the tool from its help output alone.
4. **Graph-aware planning.** Dependencies and hierarchy determine readiness and blocking. The tool computes these relationships rather than requiring manual tracking.
5. **Minimal dependencies.** Core operations require only bash and standard Unix utilities. Optional tools (jq, ripgrep) enhance specific features.
6. **Deterministic contracts.** Each command has explicit side effects, output format, and error behavior. No ambient state beyond the ticket files themselves.
7. **IDE navigability.** Ticket IDs as filenames enable Ctrl+Click navigation from commit messages and logs directly to ticket details.

---

## 3. Scope

### In Scope

- Ticket data model, lifecycle, and file format.
- All CLI commands, flags, and their behaviors.
- Dependency graph semantics (readiness, blocking, cycle detection).
- Hierarchy semantics (parent-child, status propagation).
- Analytics and reporting.
- Query/export interface.
- Migration from legacy formats.
- Environment configuration.
- Release and distribution automation.

### Out of Scope

- Networked APIs or services.
- Multi-user concurrency or locking.
- Access control or authentication.
- GUI, TUI, or interactive editors.
- Functionality not present in the current implementation.

---

## 4. Integration Context

In the `powers` agentic workflow plugin, tk is the canonical task system. The integration follows these patterns:

- **One ticket = one commit = one push.** Each unit of work produces a ticket, a code change, and a commit that includes the updated ticket file.
- **Tickets as working memory.** Agent workflows document all decisions, progress, test results, and learnings inline in the ticket via `add-note`.
- **Resumable checkpoints.** Agents embed HTML comment markers (`<!-- checkpoint: phase -->`) in ticket notes. The `show` command outputs these, enabling a subsequent session to resume from the last completed phase.
- **Workflow handoff via `needs_testing`.** Agent workflows set tickets to `needs_testing` (not `closed`) when implementation is complete. This creates a human-in-the-loop checkpoint.
- **Programmatic filtering.** The `query` command enables agents to find specific tickets via jq expressions without parsing markdown.
- **Commands used by agent workflows:** `create`, `show`, `edit`, `add-note`, `ls`, `ready`, `query`.

---

## 5. Data Model

### 5.1 Storage

- **Default directory:** `.tickets/`
- **Override:** `TICKETS_DIR` environment variable.
- **File path:** `<tickets_dir>/<ticket-id>.md`
- **Creation:** `create` and `migrate-beads` create the directory if missing. Other commands return silently when it is absent.

### 5.2 Ticket Identifier

- **Format:** `<prefix>-<hash4>` (e.g., `tk-a1b2`).
- **Prefix derivation:** First letter of each hyphen/underscore-delimited segment of the current directory name. Falls back to first 3 characters if no segments.
- **Hash:** 4-character hex substring of SHA256(PID + epoch timestamp).
- **Uniqueness:** Probabilistic. Collisions are not detected or prevented.

### 5.3 Partial ID Resolution

All commands that accept ticket IDs use this resolution algorithm:

1. Attempt exact filename match: `<tickets_dir>/<id>.md`.
2. If not found, attempt substring match: `*<id>*.md` across all files in the directory.
3. Exactly one match: resolved.
4. Multiple matches: fail with "ambiguous ID" error.
5. Zero matches: fail with "ticket not found" error.

### 5.4 File Format

```markdown
---
id: tk-a1b2
status: open
deps: []
links: []
created: 2026-01-15T10:30:00Z
type: task
priority: 2
assignee: steve
external-ref: gh-456
parent: ep-xyz1
tags: [backend, urgent]
---
# Ticket Title

Description text.

## Design

Design notes.

## Acceptance Criteria

Criteria here.

## Notes

**2026-01-15T10:35:00Z**

Note text added via add-note.
```

### 5.5 Required Frontmatter Fields

These fields are always written on creation:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | generated | Ticket identifier |
| `status` | enum | `open` | Current lifecycle state |
| `deps` | array | `[]` | Ticket IDs this ticket depends on |
| `links` | array | `[]` | Symmetrically linked ticket IDs |
| `created` | ISO 8601 | auto | UTC timestamp at creation |
| `type` | string | `task` | Ticket type |
| `priority` | integer | `2` | 0 (highest) through 4 (lowest) |

### 5.6 Optional Frontmatter Fields

| Field | Type | Description |
|-------|------|-------------|
| `assignee` | string | Defaults to `git config user.name` when available |
| `external-ref` | string | Reference to external system (e.g., `gh-123`) |
| `parent` | string | Parent ticket ID. May include trailing comment after `#` |
| `tags` | array | YAML flow array (e.g., `[ui, backend]`) |

### 5.7 Body Structure

- **Title:** First `# Heading` after frontmatter.
- **Description:** Freeform text between title and first `## Section`.
- **Sections:** Any `## Heading` creates a named section. Common sections include Design, Acceptance Criteria, and Notes. Arbitrary additional sections are permitted.

### 5.8 Status Values

| Status | Meaning |
|--------|---------|
| `open` | Not started |
| `in_progress` | Actively being worked on |
| `needs_testing` | Implementation complete, awaiting verification |
| `closed` | Done |

Status is validated on `edit`. Invalid values are rejected.

### 5.9 Priority Values

Integer range `0` through `4`. `0` is highest (critical), `4` is lowest (backlog). Validated on both `create` and `edit`.

### 5.10 Type Values

Recognized types: `task`, `epic`, `feature`, `bug`, `chore`. Type values are **not validated** — arbitrary strings are accepted.

### 5.11 Relationship Semantics

| Relationship | Directionality | Semantics |
|-------------|----------------|-----------|
| `deps` | Directional | A depends on B. A is blocked until B is `closed`. |
| `links` | Symmetric | Non-hierarchical association. Commands maintain reciprocity. |
| `parent` | Hierarchical | Single parent reference. Enables children queries and status propagation. |

---

## 6. Lifecycle and Propagation Rules

### 6.1 Status Propagation

When `edit` sets a ticket's status to `needs_testing` or `closed`:

1. Identify the ticket's parent (strip any trailing `# comment` from parent field).
2. If no parent, stop.
3. Evaluate all siblings (tickets sharing the same parent).
4. If **all siblings are `closed`** → set parent to `closed`.
5. Else if **all siblings are `needs_testing` or `closed`** → set parent to `needs_testing`.
6. Recurse: repeat from step 1 with the parent.

Propagation does **not** occur for transitions to `open` or `in_progress`.

### 6.2 Readiness Rules

A ticket is "ready" (appears in `ready` output) when all of:

1. Status is `open` or `in_progress`.
2. All entries in `deps` refer to tickets with status `closed`.
3. Every ancestor in the parent chain has status `in_progress` (hierarchy gating).

The `--open` flag on `ready` bypasses rule 3 (hierarchy gating).

### 6.3 Blocking Rules

A ticket is "blocked" (appears in `blocked` output) when:

1. Status is `open` or `in_progress`.
2. At least one entry in `deps` refers to a ticket that is not `closed`.

---

## 7. Command Specification

### 7.1 `create [title] [options]`

Creates a new ticket file.

**Options:**

| Flag | Description |
|------|-------------|
| `-d`, `--description` | Description text |
| `--design` | Design notes (creates `## Design` section) |
| `--acceptance` | Acceptance criteria (creates `## Acceptance Criteria` section) |
| `-t`, `--type` | Ticket type (default: `task`) |
| `-p`, `--priority` | Priority 0-4 (default: `2`) |
| `-a`, `--assignee` | Assignee (default: `git config user.name`) |
| `--external-ref` | External reference string |
| `--parent` | Parent ticket ID |
| `--tags` | Comma-separated tags |

**Behavior:**

- If title is omitted and stdin is a TTY, enters interactive mode: prompts for title, description, priority, type, and tags.
- If title remains empty after interactive prompts, fails with "title is required".
- Priority is validated. Invalid priority rejects creation.
- Status is always initialized as `open`.
- On success, outputs the full ticket (equivalent to `show` output), not just the ID.
- Unknown flags cause failure with error.

**Acceptance Criteria:**

- A new markdown file is created in the tickets directory.
- All required frontmatter fields are present.
- Optional fields are included only when provided or defaulted.
- `--design` and `--acceptance` create their respective `##` sections.
- `--tags` stores a YAML flow array with spaces after commas.

### 7.2 `edit <id> [options]`

Updates fields and content of an existing ticket.

**Options:**

All `create` options plus:

| Flag | Description |
|------|-------------|
| `--title` | Replace the `# Title` heading |
| `-s`, `--status` | Set status |

**Behavior:**

- Requires a ticket ID and at least one option.
- Validates status and priority when provided. Invalid values fail with non-zero exit.
- Updates YAML fields for scalar options, markdown sections for content options.
- Prints `Updated <id>` on success.
- If status is set to `needs_testing` or `closed`, triggers status propagation (Section 6.1).
- Unknown flags cause failure with error.

**Acceptance Criteria:**

- Only specified fields are modified; others are preserved.
- `--title` replaces the title heading text.
- `--description` replaces text between title and first section.
- `--design` and `--acceptance` upsert their respective sections.
- Status propagation occurs only for terminal-direction transitions.

### 7.3 `show <id>`

Displays a ticket with computed relationship context.

**Behavior:**

- Outputs the full ticket file content.
- If the ticket has a `parent` field and the parent ticket exists, decorates the parent line with the parent's title as an inline comment.
- Appends computed sections (only when non-empty):
  - **`## Blockers`**: Dependencies of this ticket that are not `closed`.
  - **`## Blocking`**: Non-closed tickets that list this ticket in their `deps`.
  - **`## Children`**: Tickets whose `parent` equals this ticket's ID.
  - **`## Linked`**: Tickets in this ticket's `links` array.

**Acceptance Criteria:**

- Original file content is reproduced exactly (with parent decoration).
- Each computed section lists entries as `- <id> [<status>] <title>`.
- Computed sections are omitted when they would be empty.

### 7.4 `delete <id> [id...]`

Deletes one or more ticket files.

**Behavior:**

- Resolves each ID and removes the corresponding file.
- Prints `Deleted <id>` for each successful deletion.
- Continues to next ID if resolution fails for one.

**Acceptance Criteria:**

- Files are removed from the tickets directory.
- Multiple IDs in a single invocation are supported.
- Does not clean up references (deps, links, parent) in other tickets.

### 7.5 `add-note <id> [text]`

Appends a timestamped note to a ticket.

**Behavior:**

- Note content comes from trailing arguments, or from stdin when piped.
- If no content is available (no args, stdin is TTY), fails with error.
- Creates `## Notes` section if missing, then appends:
  - A blank line.
  - Bold UTC timestamp: `**YYYY-MM-DDTHH:MM:SSZ**`.
  - A blank line.
  - Note content.

**Acceptance Criteria:**

- First note creates the `## Notes` section.
- Subsequent notes append without removing existing notes.
- Timestamps are UTC ISO 8601.

### 7.6 `dep <id> <dep-id>`

Adds a dependency edge: `<id>` depends on `<dep-id>`.

**Behavior:**

- Both ticket IDs must resolve.
- If dependency already exists, prints "Dependency already exists" and succeeds.
- Otherwise appends to the `deps` array.
- Prints `Added dependency: <id> -> <dep-id>`.

**Acceptance Criteria:**

- `deps` field updates from `[]` to `[dep-id]` for first dependency.
- Additional dependencies are appended: `[dep1, dep2]`.
- Duplicate addition is idempotent.
- Does not check for or prevent cycles.

### 7.7 `undep <id> <dep-id>`

Removes a dependency edge.

**Behavior:**

- Removes the dependency token from the `deps` array.
- Normalizes empty arrays to `[]`.

**Acceptance Criteria:**

- Dependency is removed from the array.
- Remaining entries are preserved.

### 7.8 `dep tree [--full] <id>`

Renders the dependency tree rooted at a ticket.

**Flags:**

| Flag | Description |
|------|-------------|
| `--full` | Show all instances of shared nodes (disables deduplication) |

**Behavior:**

- Resolves root ID (supports partial matching within awk, independent of the standard resolution path).
- Displays root as: `<id> [<status>] <title>`.
- Displays children with box-drawing characters (`├──`, `└──`).
- **Default mode:** Deduplicates nodes — each ticket appears at its deepest level only.
- **Full mode:** Every branch is shown in full, including repeated nodes.
- Children within a level are sorted by subtree depth (ascending), then by ticket ID.
- Cycles are guarded against (path tracking prevents infinite recursion).

**Acceptance Criteria:**

- Tree contains direct and transitive dependencies.
- Ambiguous root ID fails.
- Unknown root ID fails.
- Default mode shows each dependency exactly once at its deepest position.

### 7.9 `dep cycle`

Detects dependency cycles among non-closed tickets.

**Behavior:**

- Scans the dependency graph, excluding `closed` tickets.
- Uses DFS with three-color marking (white/gray/black).
- Normalizes detected cycles: rotates to start at the lexicographically smallest ID.
- Deduplicates: the same cycle is reported only once.
- If no cycles: prints `No dependency cycles found`.
- If cycles found: prints each as `Cycle N: id1 -> id2 -> ... -> id1` followed by metadata for each participant.

**Acceptance Criteria:**

- Reports at least one cycle when cycles exist.
- Duplicate representations of the same cycle are coalesced.
- Closed tickets are excluded from the scan.

### 7.10 `link <id> <id> [id...]`

Creates symmetric links among provided tickets.

**Behavior:**

- Requires at least two ticket IDs.
- Resolves all IDs first; fails if any cannot resolve.
- For each ticket, adds all other provided tickets to its `links` array.
- Skips links that already exist.
- Reports count of links added, or "All links already exist".

**Acceptance Criteria:**

- Linking two tickets writes reciprocal entries in both files.
- Linking N tickets links each to all others (all-to-all).
- Re-running with the same set is idempotent.

### 7.11 `unlink <id> <target-id>`

Removes a symmetric link between two tickets.

**Behavior:**

- Fails with "Link not found" if the link does not exist on the source ticket.
- Removes target from source's `links` and source from target's `links`.
- Prints `Removed link: <id> <-> <target-id>`.

**Acceptance Criteria:**

- After unlink, neither ticket lists the other in `links`.
- Missing link returns non-zero exit.

### 7.12 `ls` / `list [filters]`

Lists tickets in tabular format.

**Filter Flags:**

| Flag | Description |
|------|-------------|
| `--status=<X>` | Filter by status |
| `-t`, `--type=<X>` | Filter by type |
| `-P`, `--priority=<X>` | Filter by priority (leading `P` stripped) |
| `-a`, `--assignee=<X>` | Filter by assignee |
| `-T`, `--tag=<X>` | Filter by tag |
| `--group` | Shorthand for `--group-by=workflow` |
| `--group-by=<X>` | Group output. Values: `workflow`, `type`, `status`, `priority` |

**Behavior:**

- Default: lists **all tickets** (all statuses, no filter).
- Row format: `ID  P<n>  TYPE  STATUS  TITLE [<- deps]`
- Column header row is printed before results.
- Sort order (no grouping): status order → priority (ascending) → ticket ID.
- Status sort order: `in_progress` (1), `open` (2), `needs_testing` (3), `closed` (4).
- Grouped output: emits section headers (`=== Group Name ===`) with per-group column headers.
- Workflow group order: In Progress, Ready, Blocked, Needs Testing, Closed.
- Color output when stdout is TTY and `NO_COLOR` is not set: yellow for `in_progress`, cyan for `needs_testing`, green for `closed`.
- `list` is an alias for `ls`.
- Unknown flags are silently ignored.

**Acceptance Criteria:**

- Filters restrict output to matching tickets.
- Multiple filters combine as AND.
- Grouping modes produce labeled sections.

**Known constraint:** `--parent=<X>` appears in help text but is not implemented in the filter logic. It has no filtering effect.

### 7.13 `ready [filters]`

Shows tickets ready to work on.

**Filter Flags:**

| Flag | Description |
|------|-------------|
| `-a`, `--assignee=<X>` | Filter by assignee |
| `-T`, `--tag=<X>` | Filter by tag |
| `--open` | Bypass hierarchy gating (parent chain check) |

**Behavior:**

- Applies readiness rules from Section 6.2.
- Output format: `ID  [P<n>][<status>] - <title>`
- Sorted by priority (ascending), then ticket ID.
- Unknown flags are silently ignored.

**Acceptance Criteria:**

- Tickets with unresolved dependencies are excluded.
- Child tickets under non-`in_progress` parents are excluded unless `--open`.
- Only `open` and `in_progress` tickets are candidates.

### 7.14 `blocked [filters]`

Shows tickets blocked by unresolved dependencies.

**Filter Flags:**

| Flag | Description |
|------|-------------|
| `-a`, `--assignee=<X>` | Filter by assignee |
| `-T`, `--tag=<X>` | Filter by tag |

**Behavior:**

- Applies blocking rules from Section 6.3.
- Output format: `ID  [P<n>][<status>] - <title> <- [<blocker1>, <blocker2>]`
- Blocker list includes only unresolved (non-closed) dependency IDs.
- Sorted by priority (ascending), then ticket ID.
- Unknown flags are silently ignored.

**Acceptance Criteria:**

- Only tickets with at least one non-closed dependency appear.
- Only `open` and `in_progress` tickets are candidates.

### 7.15 `closed [--limit=N] [filters]`

Shows recently closed tickets.

**Filter Flags:**

| Flag | Description |
|------|-------------|
| `--limit=<N>` | Maximum results (default: `20`) |
| `-a`, `--assignee=<X>` | Filter by assignee |
| `-T`, `--tag=<X>` | Filter by tag |

**Behavior:**

- Selects the 100 most recently modified ticket files (by filesystem mtime).
- From those, filters to tickets with status `closed` (or legacy `done`).
- Outputs up to `--limit` results.
- Output format: `ID  [<status>] - <title>`
- Results ordered by file modification time (most recent first).
- Unknown flags are silently ignored.

**Acceptance Criteria:**

- Default returns at most 20 items.
- Filter flags restrict results.
- Ordering reflects filesystem modification time, not creation date.

### 7.16 `query [jq-filter]`

Emits JSONL (one JSON object per line) for all tickets.

**Behavior:**

- Each ticket produces one JSON object.
- Frontmatter scalar values are emitted as JSON strings (including `priority`).
- Frontmatter bracket arrays (`deps`, `links`, `tags`) are emitted as JSON arrays of strings.
- Body conversion:
  - First `# Heading` → `title` field.
  - Content before first `## Section` → `description` field.
  - Each `## Heading` → snake_case field key (e.g., `Acceptance Criteria` → `acceptance_criteria`).
- Frontmatter keys preserve original spelling, including hyphens (e.g., `external-ref`).
- If a jq filter is provided, it is wrapped as `jq -c "select(<filter>)"`.
- Requires `jq` only when a filter is provided.

**Acceptance Criteria:**

- With no filter, all tickets are output as JSONL.
- With a filter, only selected objects are emitted.
- JSON values are properly escaped (backslash, quotes, newlines, tabs).
- Empty body sections are omitted from JSON output.

### 7.17 `stats`

Outputs a project health dashboard.

**Behavior:**

- Reports:
  - Status breakdown (count per status).
  - Type breakdown (count per type).
  - Priority breakdown (count per priority).
  - Open ticket count, average age, and oldest ticket.
- Age calculation: days between `created` date and current date, computed via awk `mktime()`.
- If tickets directory is absent: prints `No tickets directory`.

**Acceptance Criteria:**

- All statuses/types/priorities with non-zero counts are listed.
- Age statistics require awk `mktime()` support. Environments lacking this may produce errors.

### 7.18 `timeline [--weeks=N]`

Renders a bar chart of tickets closed by week.

**Flags:**

| Flag | Description |
|------|-------------|
| `--weeks=<N>` | Number of trailing weeks to display (default: `4`) |

**Behavior:**

- Includes only tickets with status `closed` and a `created` date.
- Buckets by ISO-approximate week label (`YYYY-WNN`) derived from `created` date.
- Displays the most recent N weeks with proportional bar characters (`█`).
- If no closed tickets: prints `No closed tickets found.`
- Color: green bars when stdout is TTY and `NO_COLOR` is not set.

**Known constraint:** Timeline buckets by `created` date, not close date. There is no explicit close timestamp in the data model.

**Acceptance Criteria:**

- Chart shows weekly counts with proportional-length bars.
- Bar width scales relative to the maximum count.

### 7.19 `workflow`

Prints a static workflow guide.

**Behavior:**

- Outputs reference documentation covering: ticket types, statuses, readiness rules, status propagation rules, working conventions, and commit format.
- Content is hardcoded. No dynamic data.

**Acceptance Criteria:**

- Output contains sections for types, statuses, readiness, propagation, and conventions.

### 7.20 `migrate-beads`

Imports tickets from a legacy `.beads/issues.jsonl` file.

**Behavior:**

- Requires `.beads/issues.jsonl` file.
- Requires `jq`.
- Creates tickets directory if missing.
- For each issue, writes a ticket file.
- Dependency mapping: `blocks` → `deps`, `related` → `links`, first `parent-child` → `parent`.
- Prints `Migrated: <id>` for each ticket, then final count.

**Acceptance Criteria:**

- Each source issue produces one ticket file (overwrites existing).
- Optional fields included only when present and non-empty.

### 7.21 `help` / `--help` / `-h`

Prints command usage reference and examples.

**Behavior:**

- Displays all command categories, flag summaries, query examples, and partial ID matching description.
- Default command when invoked with no arguments.

**Acceptance Criteria:**

- Unknown commands print error to stderr, print help to stderr, and exit non-zero.

---

## 8. Query Contract for Automation

The `query` command's JSONL output serves as the machine-consumption interface. These are the guarantees consumers can rely on:

- **Format:** JSON Lines — one JSON object per line, no surrounding array.
- **Scalar typing:** All frontmatter scalar values are emitted as JSON strings, including numeric fields like `priority`.
- **Array typing:** YAML-style bracket arrays (`deps`, `links`, `tags`) become JSON arrays of strings.
- **Key naming:**
  - Frontmatter keys preserve original spelling, including hyphens (e.g., `external-ref`).
  - Body section keys are lowercase snake_case (e.g., `Acceptance Criteria` → `acceptance_criteria`).
- **Presence:** Body sections appear in JSON only when they contain content.
- **Filtering:** The optional jq filter is wrapped in `select()` automatically. Consumers must not add their own `select()`.

---

## 9. Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `TICKETS_DIR` | `.tickets` | Directory for ticket storage |
| `NO_COLOR` | unset | When set, disables ANSI color output (respects the [NO_COLOR standard](https://no-color.org/)) |

---

## 10. Dependencies

### Required

- bash
- Standard POSIX utilities: sed, awk, find, date, ls, xargs, head, mkdir, mv, rm, basename, printf

### Optional

| Tool | Used By | Fallback |
|------|---------|----------|
| `rg` (ripgrep) | Internal search operations | `grep` |
| `jq` | `query` (with filter), `migrate-beads` | No fallback (commands fail without it) |

### Platform Portability

The tool detects and adapts to platform differences:

- **SHA256:** `sha256sum` (Linux) or `shasum -a 256` (macOS).
- **sed in-place:** Implemented via temp file + mv (avoids BSD/GNU `-i` differences).
- **Date formatting:** `date -u +%Y-%m-%dT%H:%M:%SZ` (POSIX-portable).

---

## 11. Output Formats

### 11.1 `ls` / `list`

```
ID        P<n>  TYPE        STATUS         TITLE [<- deps]
tk-a1b2   P2    task        open           My ticket title
tk-c3d4   P1    feature     in_progress    Another ticket <- [tk-a1b2]
```

### 11.2 `ready` / `blocked`

```
tk-a1b2  [P2][open] - My ticket title
tk-c3d4  [P1][in_progress] - Another ticket <- [tk-e5f6, tk-g7h8]
```

### 11.3 `closed`

```
tk-a1b2  [closed] - My ticket title
```

### 11.4 `dep tree`

```
tk-root [in_progress] Root ticket
├── tk-dep1 [closed] First dependency
└── tk-dep2 [open] Second dependency
    └── tk-dep3 [open] Transitive dependency
```

### 11.5 `dep cycle`

```
Cycle 1: tk-a1b2 -> tk-c3d4 -> tk-a1b2
  tk-a1b2  [open] First ticket
  tk-c3d4  [open] Second ticket
```

### 11.6 `query`

```json
{"id":"tk-a1b2","status":"open","deps":[],"links":[],"created":"2026-01-15T10:30:00Z","type":"task","priority":"2","title":"My ticket","description":"Some text","design":"Design notes"}
```

### 11.7 `stats`

```
  PROJECT HEALTH

  Status:
    open            5
    in_progress     3
    needs_testing   1
    closed          12
    TOTAL           21

  Types:
    epic            2
    feature         5
    task            10
    bug             3
    chore           1

  Priority:
    P0              1
    P1              3
    P2              12
    P3              4
    P4              1

  Open Tickets:
    Count           9
    Average age     14 days
    Oldest          45 days (tk-a1b2)
```

### 11.8 `timeline`

```
  TICKETS CLOSED BY WEEK

  2026-W03  ████████████ 6
  2026-W04  ████ 2
  2026-W05  ██████████████████████████████ 15
  2026-W06  ██ 1
```

---

## 12. Error Handling

### Exit Codes

- **0:** Success.
- **Non-zero:** Error (invalid input, ticket not found, ambiguous ID, unknown command).

### Error Categories

| Category | Behavior |
|----------|----------|
| Missing required argument | Non-zero exit, usage hint to stderr |
| Invalid status value | Non-zero exit, lists valid values |
| Invalid priority value | Non-zero exit, lists valid values |
| Ticket not found | Non-zero exit, error message to stderr |
| Ambiguous partial ID | Non-zero exit, error message to stderr |
| Unknown command | Non-zero exit, error + help to stderr |
| Unknown flag (create/edit) | Non-zero exit, error message |
| Unknown flag (ls/ready/blocked/closed/timeline) | Silently ignored |
| Missing tickets directory | Commands return 0 with no output |

### Option Handling Families

Commands fall into two groups for unknown flag behavior:

- **Strict:** `create`, `edit`. Unknown flags cause non-zero exit with error. These are mutation commands where a mistyped flag could silently drop intended changes.
- **Permissive:** `ls`, `ready`, `blocked`, `closed`, `timeline`. Unknown flags are silently consumed. These are read-only commands where strictness would break forward-compatible scripting.

---

## 13. Command Dispatch

| Input | Command |
|-------|---------|
| `create` | `create` |
| `delete` | `delete` |
| `dep` | Routes to `dep`, `dep tree`, or `dep cycle` based on subcommand |
| `undep` | `undep` |
| `link` | `link` |
| `unlink` | `unlink` |
| `ls` or `list` | `ls` |
| `ready` | `ready` |
| `blocked` | `blocked` |
| `closed` | `closed` |
| `show` | `show` |
| `edit` | `edit` |
| `add-note` | `add-note` |
| `query` | `query` |
| `stats` | `stats` |
| `timeline` | `timeline` |
| `workflow` | `workflow` |
| `migrate-beads` | `migrate-beads` |
| `help`, `--help`, `-h` | `help` |
| (no arguments) | `help` |
| (unknown) | Error + help to stderr |

---

## 14. Release and Distribution

The repository includes CI automation (`.github/workflows/release.yml`):

- **Trigger:** Push of a tag matching `v*`.
- **GitHub Release:** Extracts the corresponding section from `CHANGELOG.md` as the release body. Computes tarball SHA256.
- **Homebrew:** Updates the formula in the `wedow/homebrew-tools` tap.
- **AUR:** Updates the `ticket` AUR package metadata, including `.SRCINFO` generation via Docker.

The script is typically installed as `tk` in the user's PATH.

---

## 15. Known Behavioral Constraints

These are observable behaviors that may differ from documentation or expectations. They are captured here as normative (as-is) behavior.

1. **`ls` default scope.** Help text says "default: open only" but `ls` with no `--status` flag includes all statuses. There is no default status filter.
2. **`--parent` filter for `ls`.** Documented in help text as a filter flag but not implemented in the command's argument parser. Has no filtering effect.
3. **Type validation.** Type values are not validated by `create` or `edit`. Arbitrary strings are accepted and stored.
4. **`query` types.** All frontmatter values (including `priority`) are emitted as JSON strings, not numbers.
5. **`query` key spelling.** Frontmatter keys preserve hyphens (e.g., `external-ref`). Body section keys use snake_case (e.g., `acceptance_criteria`).
6. **`timeline` bucketing.** Closed tickets are bucketed by `created` date, not close date. The data model has no close timestamp.
7. **`stats` portability.** Age calculations depend on awk `mktime()`, which is a gawk extension. May fail on minimal awk implementations.
8. **`closed` status matching.** The `closed` command also matches the legacy status value `done`.

---

## 16. Conformance Checklist

An implementation conforms to this specification if all of the following hold:

1. Tickets are stored as Markdown + YAML files in a configurable local directory.
2. Partial ID resolution works with ambiguity protection (Section 5.3).
3. CRUD commands (`create`, `show`, `edit`, `delete`, `add-note`) preserve behavior described in Section 7.
4. Dependency commands (`dep`, `undep`, `dep tree`, `dep cycle`) preserve graph semantics.
5. Link commands (`link`, `unlink`) preserve symmetric behavior.
6. Queue commands (`ls`, `ready`, `blocked`, `closed`) preserve filtering, sorting, and output semantics.
7. Hierarchy gating and status propagation rules (Section 6) are intact.
8. `query` JSONL shape, key naming, and filtering semantics are preserved.
9. Analytics commands (`stats`, `timeline`) produce equivalent reports.
10. `workflow` and `help` output covers all commands and options.
11. Environment variable behavior (`TICKETS_DIR`, `NO_COLOR`) is honored.
12. Error handling contracts (Section 12) are respected.
13. Query contract (Section 8) is preserved.
14. Constraints in Section 15 are explicitly acknowledged unless intentionally changed.
