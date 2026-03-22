---
id: create-central-tickets-3a39
stage: implement
risk: low
deps: []
links: []
created: 2026-03-22T04:49:08Z
type: feature
priority: 2
parent: eb6c
tags: [architecture, tk, storage]
---
# Create central tickets repo and migrate existing tickets

Set up a dedicated git repo for centralized ticket storage. Migrate all existing .tickets/ directories from project repos into the central store. Configure TICKETS_DIR to point all consumers at the new location.

**Scope:**
- Create the central repo
- Write a migration script to collect tickets from project repos
- Run the migration
- Set TICKETS_DIR for all consumers (Forge, pipeline, CLI)

## Acceptance Criteria

1. When the migration script runs, it shall create ~/code/forge/data/tickets/ with per-project subdirectories, verified by: `ls ~/code/forge/data/tickets/` shows forge/, ticket/, and ghostwheel/ subdirectories.

2. When the migration script copies tickets from a project repo, it shall copy every .md file from <project>/.tickets/ into ~/code/forge/data/tickets/<project>/, verified by: file counts match between source and target for each project.

3. When the migration script copies a ticket file, the file content shall be byte-identical to the source, verified by: `diff` on a sample of files shows no differences.

4. When the migration completes, the script shall commit all migrated tickets in the forge-data repo, verified by: `git -C ~/code/forge/data log --oneline -1` shows a migration commit.

5. When the migration completes, the forge-data repo remote shall remain pointing to EnderRealm/forge-data, verified by: `git -C ~/code/forge/data remote -v` shows the correct origin.

6. When TICKETS_DIR is set to a project subdirectory (e.g., ~/code/forge/data/tickets/ticket), tk commands shall use that directory for all ticket operations, verified by: `TICKETS_DIR=~/code/forge/data/tickets/ticket tk list` from a directory with no .tickets/ returns the migrated tickets.

7. When TICKETS_DIR is set and tk serve is started, the MCP server shall operate against the TICKETS_DIR path, verified by: `TICKETS_DIR=~/code/forge/data/tickets/ticket tk serve` and calling ticket_list via MCP returns the same tickets as criterion 6.

8. When TICKETS_DIR is not set, tk shall fall back to existing behavior (walk up from CWD to find .tickets/), verified by: unset TICKETS_DIR, run tk list from inside a project repo with .tickets/, confirm it finds tickets via walk-up.

## Design

## Architecture

No tk code changes required. `TICKETS_DIR` env var (`cmd/root.go:196`) returns the value directly — no `.tickets/` suffix appended. The MCP server uses the same `TicketsDir()` function via `cmd/serve.go`, which passes the dir to `ticket.NewFileStore()`. All ticket operations flow through `FileStore`, so pointing `TICKETS_DIR` at a different path works end-to-end.

Tickets are added to the existing `forge-data` repo at `~/code/forge/data/` (remote: `EnderRealm/forge-data`). A `tickets/` directory is created inside it with per-project subdirectories.

**Path structure:**
```
~/code/forge/data/
  tickets/
    forge/     (325 tickets)
    ticket/    (210 tickets)
    ghostwheel/ (334 tickets)
```

## Implementation Plan

### Step 1: Write migration script

File: `~/code/forge/data/migrate-tickets.sh` (temporary)

```bash
#!/bin/bash
set -euo pipefail

CENTRAL="$HOME/code/forge/data/tickets"
PROJECTS=("forge:$HOME/code/forge" "ticket:$HOME/code/ticket" "ghostwheel:$HOME/code/ghostwheel")

for entry in "${PROJECTS[@]}"; do
    name="${entry%%:*}"
    path="${entry#*:}"
    mkdir -p "$CENTRAL/$name"
    src="$path/.tickets"
    if [ -d "$src" ]; then
        cp "$src"/*.md "$CENTRAL/$name/"
        echo "Copied $(ls "$src"/*.md | wc -l | tr -d ' ') tickets from $name"
    else
        echo "Warning: $src not found, skipping"
    fi
done

cd "$HOME/code/forge/data"
git add tickets/
git commit -m "Add centralized ticket store with migrated tickets"
echo "Migration complete."
```

### Step 2: Run migration and verify

- Execute script
- Compare file counts: source vs target per project
- Spot-check byte identity with `diff`

### Step 3: Configure TICKETS_DIR

Add to `~/.zshenv` (or equivalent):
```bash
export TICKETS_DIR="$HOME/code/forge/data/tickets/ticket"
```

For per-project use, consumers set TICKETS_DIR to the project subdir. Forge consumer config is out of scope (ticket 6a21).

### Step 4: Push

Push the forge-data repo to persist the migration.

## Files Affected

| Location | Change |
|----------|--------|
| `~/code/forge/data/migrate-tickets.sh` | New — migration script (temporary) |
| `~/code/forge/data/tickets/{forge,ticket,ghostwheel}/*.md` | New — migrated tickets |
| `~/.zshenv` | Modified — add TICKETS_DIR export |

No changes to the tk codebase.

## Key Decisions
- Use existing forge-data repo, not a new repo (human)
- Per-project subdirectories to avoid ID collisions (human)
- Shell script for migration, not a tk subcommand (auto)
- TICKETS_DIR points to project-specific subdir (human)
- Forge consumer updates deferred to ticket 6a21 (human)

## Review Log

**2026-03-22T05:13:26Z [human:steve]**
APPROVED — Spec approved. 9 AC covering repo creation, migration, and TICKETS_DIR verification. Forge changes deferred to 6a21.

**2026-03-22T19:28:10Z [agent:design-reviewer]**
APPROVED — TICKETS_DIR works end-to-end without code changes — verified cmd/root.go:196, cmd/serve.go, internal/mcp/mcp.go:17-18, pkg/ticket/store.go. All three source repos confirmed to have .tickets/ with .md files. Hardcoded .tickets in move.go:30 and tui.go:654 noted but not affected by TICKETS_DIR (separate tk move concern).

**2026-03-22T19:28:59Z [human:steve]**
APPROVED — Design approved. Using existing forge-data repo, shell script migration, no tk code changes.

## Notes

**2026-03-22T04:55:47Z**

## Triage

**Risk:** low — File/repo reorganization only. No auth, PII, schema, or API changes. TICKETS_DIR env var already supported in tk.

**Priority:** 2 — Foundation for the central-store epic but not blocking production work today.

**Scope:** single task

**Key decisions:**
- Central repo location: EnderRealm/ForgeData, under a `tickets/` directory at ~/code/forge/tickets/ (human)
- TICKETS_DIR will point all consumers at ~/code/forge/tickets/ (human)
- Eventually merges with Forge's data directory (human)

**2026-03-22T05:12:57Z**

## Spec

**Scope:**
- In: repo creation, migration script, TICKETS_DIR configuration, verification that tk CLI and MCP work against central store
- Out: Forge consumer changes (covered by update-forge-dashboard-6a21), removing .tickets from project repos (covered by clean-project-tickets-750b)

**Decisions:**
- Per-project subdirectories under central root, matching tkt pattern (human)
- TICKETS_DIR points to project-specific subdir, no tk code changes needed (human)
- Forge consumer updates are separate ticket 6a21 (human)
- Migration is a shell script, not a tk subcommand (auto)

**2026-03-22T19:28:14Z**

## Design

**Approach:** Add tickets/ directory to existing forge-data repo (~/code/forge/data/) with per-project subdirectories. Shell script migration, no tk code changes. TICKETS_DIR env var handles routing.

**Files affected:**
- ~/code/forge/data/migrate-tickets.sh (new, temporary)
- ~/code/forge/data/tickets/{forge,ticket,ghostwheel}/*.md (new)
- ~/.zshenv (TICKETS_DIR export)

**Review:** agent:design-reviewer: approved — TICKETS_DIR verified end-to-end, all source repos confirmed

**AC6 amendment needed:** Remote is EnderRealm/forge-data (existing), not EnderRealm/ForgeData. AC1-2 paths should reference ~/code/forge/data/tickets/ not ~/code/forge/tickets/.
