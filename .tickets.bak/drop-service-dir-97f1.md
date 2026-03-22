---
id: drop-service-dir-97f1
stage: backlog
risk: high
deps: [central-store-configuration-5b5b, central-store-migration-f33d, central-store-git-d862]
links: []
created: 2026-03-22T04:01:27Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [cleanup, storage]
---
# Drop service-dir clone and worktree ticket sync

Once central store is working, remove the legacy sync mechanisms that the central store makes unnecessary.

**What to remove:**
- Service directory clone logic (ensureServiceDir and related functions) — central store eliminates the need for cloned copies
- Worktree ticket file sync in the orchestrator — tickets are no longer in project branches
- Stash/pull/push cycles for ticket reads — central store is always available without git gymnastics
- Debounce timers for ticket writes — append-only is simpler than conflict resolution

**What to update:**
- Forge dashboard reads from central store directly (no clone needed)
- All three writers (Forge interactive, Forge pipeline, CLI) point at `~/.tickets/<project>/`
- Project `.gitignore` entries for `.tickets/` can be removed
- Any remaining in-project `.tickets/` directories can be deleted after migration

**Depends on:** central-store-configuration-5b5b, central-store-migration-f33d, central-store-git-d862

**Validation:** All three writers successfully create, edit, and advance tickets against the central store. No ticket data in project git history.
