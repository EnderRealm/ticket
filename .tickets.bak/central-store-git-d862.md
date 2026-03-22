---
id: central-store-git-d862
stage: backlog
risk: high
deps: [central-store-configuration-5b5b, commit-journal-background-e2be]
links: []
created: 2026-03-22T03:59:40Z
type: task
priority: 2
parent: migrate-tk-storage-eb6c
tags: [architecture, storage, tkt-port]
---
# Central store git sync in daemon

Add git auto-sync for the central ticket store. When tickets live in `~/.tickets/<project>/`, the daemon auto-commits changes and pushes to a remote (if configured). This eliminates the service-dir clone sync and worktree file copy problems.

**What to build:**
- Daemon detects changed ticket files in central store on each cycle
- `git add -A` + `git commit` with message listing affected ticket IDs (e.g., "tk: sync p-123, p-456")
- `git fetch` + `git merge --ff-only` before committing local changes (pre-sync)
- `git push` after commit, with retry via `pull --rebase` on failure
- Sync-blocked state persisted in `.git/tk-central-sync-blocked` — daemon skips push until resolved
- Block auto-clears when rebase state and unpushed commits are resolved

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Sync trigger | `internal/cli/watch_commands.go:109-121` | Checks `entry.Store == "central"`, calls `syncCentralStoreGit()` |
| Main sync | `internal/cli/watch_commands.go:691-793` | `syncCentralStoreGit()` — ensure git, check blocked, pre-sync, commit, push |
| Pre-sync fetch+merge | `internal/cli/watch_commands.go:828-878` | `preSyncCentralStoreRemote()` — `git fetch`, `git merge --ff-only` |
| Commit message | `internal/cli/watch_commands.go:1018-1038` | `buildCentralCommitMessage()` — extracts ticket IDs from filenames |
| Push with retry | `internal/cli/watch_commands.go:749-792` | Try push, fall back to `pull --rebase`, retry push, block on failure |
| Push helper | `internal/cli/watch_commands.go:908-924` | `pushCentralStoreGit()` — handles upstream setup with `-u` |
| Block read/write/clear | `internal/cli/watch_commands.go:942-999` | `.git/tkt-central-sync-blocked` file management |
| Block resolution | `internal/cli/watch_commands.go:1001-1016` | `centralSyncBlockResolved()` — checks no rebase in progress, clean status, no unpushed |

**Adaptation notes:**
- This depends on the daemon from the commit journal ticket — can share the same `tk serve` process
- The daemon runs both commit watching and central store sync on the same interval
- Git identity set to `tk@local` / `tk` for automated commits
