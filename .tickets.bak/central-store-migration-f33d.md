---
id: central-store-migration-f33d
stage: backlog
risk: high
deps: [central-store-configuration-5b5b]
links: []
created: 2026-03-22T03:58:59Z
type: task
priority: 1
parent: migrate-tk-storage-eb6c
tags: [architecture, storage, tkt-port]
---
# Central store migration command

Add `tk migrate --central|--local` command to move tickets between storage modes. This is the escape hatch — users can switch without data loss.

**What to build:**
- `tk migrate --central` moves `.tickets/*.md` to `~/.tickets/<project>/`
- `tk migrate --local` moves `~/.tickets/<project>/*.md` back to `.tickets/`
- `--yes` flag skips confirmation prompt
- Update `~/.tk/config.yaml` store field after successful move
- Remove empty source directory after move
- Validate source exists and has tickets before proceeding

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Migrate command | `internal/cli/migrate_recompute_commands.go:21-132` | `runMigrate()` — flag parse, path resolution, file move, config update |
| File collection | Same file, line ~61 | `ticketFiles(source)` — finds all `.md` files |
| File movement | Same file, lines 104-110 | `moveFilesByName()` — atomic move of ticket files |
| Cleanup | Same file, line ~110 | `removeDirIfEmpty()` — removes source if empty after move |
| Config update | Same file, lines 113-117 | Updates `entry.Store` and calls `project.Save()` |

**Adaptation notes:**
- tk already has a `migrate` command (`cmd/migrate.go`) for moving tickets between repos — this is a different operation (storage mode switch). Consider naming it `tk migrate-store --central|--local` to avoid collision, or extend existing migrate with a `--store` flag.
