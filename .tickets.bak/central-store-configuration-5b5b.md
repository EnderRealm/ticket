---
id: central-store-configuration-5b5b
stage: test
risk: normal
deps: []
links: []
created: 2026-03-22T03:58:50Z
type: feature
priority: 1
parent: migrate-tk-storage-eb6c
tags: [architecture, storage, tkt-port]
---
# Central store configuration and init

Add central store support so tickets live in `~/.tickets/<project>/` instead of `.tickets/` inside each project repo. tk already has `TICKETS_DIR` env var support — this formalizes it with per-project config and an init flow.

**What to build:**
- Global config file at `~/.tk/config.yaml` with per-project settings (path, store type, auto_link, auto_close)
- `tk init --store central|local` command to set up a project
- `TICKETS_DIR` env var override for central root (must be absolute path, defaults to `~/.tickets`)
- Project name resolution: explicit flag > config path match > git remote name > directory name
- Bootstrap central store as a git repo on first init

**tkt reference implementation:**

| What | File (relative to `~/code/tkt/`) | Key functions |
|------|------|------|
| Config struct | `internal/project/config.go:23-29` | `ProjectConfig` with Path, Store, AutoLink, AutoClose, RegisteredAt |
| Config load/save | `internal/project/config.go:31-68` | `Load()`, `Save()`, `ConfigPath()` returns `~/.tkt/config.yaml` |
| Config parse | `internal/project/config.go:95-165` | `parseConfig()` — custom YAML parser |
| Central root | `internal/engine/paths.go:27-42` | `CentralStoreRoot()` — checks `TKT_ROOT` env, falls back to `~/.tickets` |
| Central project dir | `internal/engine/paths.go:44-51` | `CentralProjectDir()` — `<root>/<projectName>` |
| Init command | `internal/cli/init_config_commands.go:20-262` | `runInit()` — interactive store selection, bootstraps git |
| Git bootstrap | `internal/cli/init_config_commands.go:268-302` | `bootstrapCentralStoreGit()` — git init, set identity, initial commit |
| Project resolution | `internal/project/resolve.go` | Precedence: flag > config path > git remote > dir name |

**Adaptation notes:**
- tk uses Cobra, not custom dispatcher — wire as `tk init` subcommand
- tk's env var is `TICKETS_DIR` — keep that, map to same semantics as tkt's `TKT_ROOT`
- Config file at `~/.tk/config.yaml` (not `~/.tkt/`) to avoid collision during transition

## Acceptance Criteria

1. When `tk init --store central` is run in a git repository, the system shall create `~/.tk/config.yaml` with a project entry containing path, store=central, auto_link=false, auto_close=false, and registered_at fields, verified by `go test ./internal/project/ -run TestConfig`.

2. When `tk init --store local` is run, the system shall create `~/.tk/config.yaml` with store=local and ensure `.tickets/` exists under the project root, verified by `go test ./internal/project/ -run TestConfig`.

3. When `tk init --store central` is run and `~/.tickets/` does not yet contain a `.git` directory, the system shall run `git init`, set a local git identity (tk@local / tk), stage all files, and create an initial commit, verified by `go test ./cmd/ -run TestInitBootstrapGit`.

4. When `tk init --store central` is run and `.tickets/` already exists locally, the system shall copy all `.md` files from `.tickets/` to `~/.tickets/<project>/` (preserving originals), verified by `go test ./cmd/ -run TestInitCopyLocal`.

5. When `~/.tk/config.yaml` exists with a project entry where store=central and path matches the current working directory, `TicketsDir()` shall return `~/.tickets/<project>/`, verified by `go test ./cmd/ -run TestTicketsDirCentral`.

6. When `TICKETS_DIR` env var is set, `TicketsDir()` shall return its value regardless of config, preserving existing precedence (--repo flag > TICKETS_DIR > config lookup > walk-up > fallback), verified by `go test ./cmd/ -run TestTicketsDirPrecedence`.

7. When `TICKETS_DIR` is set to a relative path, `TicketsDir()` shall use it as-is (existing behavior preserved), verified by `go test ./cmd/ -run TestTicketsDirPrecedence`.

8. When the `--project` flag is provided to `tk init`, the system shall use that name instead of auto-resolving from config/git-remote/dirname, verified by `go test ./cmd/ -run TestInitProjectFlag`.

9. When no `--project` flag is provided and no config match exists, the system shall resolve the project name from git remote origin (extracting repo name from URL), falling back to directory name, verified by `go test ./internal/project/ -run TestResolveName`.

10. When `tk init --store central --yes` is run (non-interactive mode), the system shall skip all prompts and default to central store, verified by `go test ./cmd/ -run TestInitNonInteractive`.

11. When `tk init` is run with `--json`, the system shall output a JSON object containing project, path, store, config, has_git, and copied_local_to_central fields, verified by `go test ./cmd/ -run TestInitJSON`.

12. When `~/.tk/config.yaml` is missing or empty, `project.Load()` shall return an empty Config (not an error), verified by `go test ./internal/project/ -run TestLoadMissing`.

13. When `~/.tk/config.yaml` contains malformed YAML, `project.Load()` shall return a descriptive error, verified by `go test ./internal/project/ -run TestLoadMalformed`.

14. When `tk init --store invalid` is run, the system shall return an error message "store must be central or local", verified by `go test ./cmd/ -run TestInitInvalidStore`.

## Design

## Architecture

Three new pieces slot into the existing structure:

1. **`internal/project/`** (new package) — Config types, load/save, project name resolution. Ported from tkt with adaptations.
2. **`cmd/init.go`** (new command) — `tk init` Cobra subcommand. Store selection, config registration, local-to-central copy, git bootstrap.
3. **`cmd/root.go` modification** — Config-based resolution in `TicketsDir()` between `TICKETS_DIR` env and walk-up fallback.

## Data Flow

```
tk init --store central
  → resolve project name (flag > config > git remote > dirname)
  → create ~/.tickets/<project>/
  → copy .tickets/*.md if present
  → git init ~/.tickets/ if needed
  → write project entry to ~/.tk/config.yaml

tk <any command>
  → TicketsDir()
    → --repo flag? use it
    → TICKETS_DIR env? use it
    → config match for CWD? return ~/.tickets/<project>/   ← NEW
    → walk up for .tickets/? use it
    → fallback .tickets
  → NewFileStore(dir)
```

## Implementation Plan

**Step 1: `internal/project/config.go`**
- Config dir: `~/.tk/` (not `~/.tkt/`)
- Use `gopkg.in/yaml.v3` (already in go.mod) instead of custom parser
- Types: `Config{Projects map[string]ProjectConfig}`, `ProjectConfig{Path, Store, AutoLink, AutoClose, RegisteredAt}`
- Functions: `Load()`, `Save()`, `ConfigPath()`, `UpsertProject()`
- `CentralStoreRoot()` — checks `TICKETS_DIR` env, falls back to `~/.tickets`
- `CentralProjectDir(projectName)` — `<root>/<projectName>`
- Atomic write (temp file + rename) to prevent corruption

**Step 2: `internal/project/resolve.go`**
- `ResolveName(cfg, cwd, explicit) (name, source)` — 4-tier precedence
- `DetectProjectPath(cwd)` — git top-level or cwd
- Helpers: `matchProjectByPath`, `projectFromGitRemote`, `projectFromDir`, `gitRoot`, `canonicalPath`

**Step 3: Tests for `internal/project/`**
- `TestConfig` — Save/Load round-trip, UpsertProject (AC 1, 2)
- `TestLoadMissing` — missing file returns empty Config (AC 12)
- `TestLoadMalformed` — bad YAML returns error (AC 13)
- `TestResolveName` — all 4 tiers (AC 9)

**Step 4: `cmd/init.go`**
- Cobra command, flags: `--store`, `--project`, `--yes`, `--json`
- Validate --store (AC 14)
- Resolve project name (AC 8, 9)
- Central: create dir, copy local .md files (AC 4), bootstrap git (AC 3)
- Local: ensure .tickets/ (AC 2)
- No --store + --yes: default central (AC 10)
- Config upsert + save (AC 1)
- JSON output (AC 11)
- Git bootstrap: git init if no .git, local identity tk@local/tk, stage, commit

**Step 5: `cmd/root.go`**
- Insert `ticketsDirFromConfig()` between TICKETS_DIR env and walk-up
- Load config, match project by CWD, return central dir if store=central
- Silent fallthrough on error (AC 5, 6, 7)

**Step 6: `cmd/init_test.go`**
- TestInitBootstrapGit, TestInitCopyLocal, TestTicketsDirCentral, TestTicketsDirPrecedence, TestInitProjectFlag, TestInitNonInteractive, TestInitJSON, TestInitInvalidStore
- Tests use temp dirs for HOME isolation

**Step 7: Help text + README**
- Add `tk init` to help text
- Document in README Usage section

## Key Decisions

| Decision | Rationale |
|----------|-----------|
| yaml.v3 over custom parser | Already in go.mod, less code, handles edge cases |
| CentralStoreRoot in internal/project/ | Both init and TicketsDir need it |
| Skip auto-link/auto-close prompts | Daemon out of scope, default false |
| ticketsDirFromConfig() silent fallthrough | Config failure shouldn't break existing users |
| Atomic config write (temp + rename) | Prevents corruption on partial write |

## Review Log

**2026-03-22T19:04:32Z [human:steve]**
APPROVED — Spec approved. 14 AC covering init command, config CRUD, TicketsDir precedence, project resolution, and edge cases.

**2026-03-22T19:16:07Z [agent:design-reviewer]**
APPROVED — READY. All 14 AC covered. File paths verified. Insertion point in TicketsDir() confirmed. yaml.v3 in go.mod. Cobra pattern matches existing commands. No existing internal/project/ directory. Four minor warnings addressable during implementation.

**2026-03-22T19:20:36Z [human:steve]**
APPROVED — Design approved.

**2026-03-22T19:20:53Z [human:steve]**
APPROVED — Design approved at design-review gate.

**2026-03-22T19:31:52Z [agent:code-reviewer]**
APPROVED — Approved after fixes. Critical: removed TICKETS_DIR overload from CentralStoreRoot. Security: added project name sanitization. Fixed git add error handling, updated doc comment. All findings addressed.

**2026-03-22T19:31:59Z [agent:impl-reviewer]**
APPROVED — Approved after fixes. All 14 AC covered. CentralStoreRoot test fixed. Three fake tests rewritten to call runInit directly. go.mod tidied. README --json flag added.

**2026-03-22T19:32:23Z [agent:code-reviewer]**
APPROVED — Code review passed. All findings from both reviewers addressed.

## Notes

**2026-03-22T18:16:01Z**

## Triage

**Risk:** normal — Additive change to core path resolution (TicketsDir). No auth, PII, schema, or API contract changes. Main risk is regression in the TicketsDir chokepoint, catchable with tests.

**Priority:** 1 — Foundation ticket for the central-store epic. Sibling ticket 3a39 (migrate) depends on this config/init infrastructure.

**Scope:** single task

**Key decisions:**
- Risk normal, not high — change is additive, not destructive (human)
- Priority stays at 1 (human)

**2026-03-22T19:03:39Z**

## Spec

**Scope:**
- In: New `internal/project/` package (config.go, resolve.go + tests), new `cmd/init.go`, update `cmd/root.go` TicketsDir() resolution, update README/help text
- Out: `tk config` subcommands, auto_link/auto_close daemon logic, MCP/TUI changes (already receive resolved paths), concurrent writer safety, auto-migration

**Decisions:**
- auto_link and auto_close stored in config but inert — daemon is out of scope (auto)
- Config at `~/.tk/config.yaml` to avoid collision with tkt (human)
- Central root defaults to `~/.tickets` via TICKETS_DIR semantics (human)
- 14 AC covering init command, config CRUD, TicketsDir precedence, project resolution, edge cases (human)

**2026-03-22T19:16:46Z**

## Design

**Approach:** Port tkt's central store infrastructure (config, resolve, init) to tk using yaml.v3 and Cobra. Insert config-based resolution into TicketsDir() between TICKETS_DIR env and walk-up fallback.

**Files affected:**
- internal/project/config.go (new)
- internal/project/resolve.go (new)
- internal/project/config_test.go (new)
- internal/project/resolve_test.go (new)
- cmd/init.go (new)
- cmd/init_test.go (new)
- cmd/root.go (modify TicketsDir)
- README.md (document tk init)

**Review:** agent:design-reviewer: approved — all 14 AC covered, file paths verified, no blockers

**2026-03-22T19:32:07Z**

## Implement

**Changes:**
- internal/project/config.go: Config types, Load/Save with yaml.v3, atomic write, CentralStoreRoot, CentralProjectDir
- internal/project/resolve.go: ResolveName with 4-tier precedence, project name sanitization
- internal/project/config_test.go: 9 tests covering round-trip, missing, malformed, paths
- internal/project/resolve_test.go: 6 tests covering all resolution tiers and path traversal
- cmd/init.go: tk init command with --store, --project, --yes, --json; git bootstrap; file copy
- cmd/init_test.go: 8 tests exercising real runInit code paths
- cmd/root.go: ticketsDirFromConfig() inserted in TicketsDir() chain, help text updated
- README.md: init command documented
- CHANGELOG.md: entries added

**Decisions:**
- CentralStoreRoot does NOT check TICKETS_DIR (auto) — different semantics, would corrupt user dirs
- Project names sanitized to reject path traversal (auto) — security fix from code review
- git add -A error now checked (auto) — surfacing git failures instead of swallowing

**Reviews:**
- code-reviewer: approved (after fixing critical TICKETS_DIR overload, security path traversal, error handling)
- impl-reviewer: approved (after fixing fake tests, go.mod tidy, README consistency)
