---
id: tui-hybrid-refresh-5c6f
stage: backlog
risk: normal
deps: [central-store-configuration-5b5b]
links: []
created: 2026-03-22T18:19:57Z
type: task
priority: 3
parent: tui-revamp-port-0365
tags: [tui, tkt-port]
---
# TUI hybrid refresh — fsnotify plus periodic poll

Upgrade ticket's file-watch-only refresh to a hybrid approach: fsnotify for immediate local changes plus periodic polling for central store changes that fsnotify won't catch (daemon commits, remote pushes from other machines).

**Current state:**
- ticket uses fsnotify (`internal/tui/watcher.go`) with 200ms debounce
- Works well for local `.tickets/` directory
- Will miss changes from daemon auto-sync and remote pushes to central store

**What to build:**
- Keep fsnotify as primary refresh for sub-second local responsiveness
- Add periodic poll (every 2-5 seconds) as fallback for central store
- Smart guards: skip poll during overlays, form editing, text input, review input (same as tkt)
- Pending refresh flag: if poll fires during blocked state, execute on unblock
- Preserve cursor position, scroll offset, and selection across refreshes

**tkt reference implementation:**

| What | File | Key functions |
|------|------|------|
| Poll interval | `internal/tui/app.go` | `defaultPollInterval = 2 * time.Second` |
| Auto-refresh cmd | `internal/tui/app.go` | `autoRefreshTickCmd` — tea.Tick with interval |
| Guard check | `internal/tui/app.go` | `canAutoRefresh()` — not in overlay, editor, filter, picker, loading |
| Pending flag | `internal/tui/app.go` | `refreshPending` set when blocked, `flushPendingLoad` on unblock |
| State preservation | `internal/tui/board.go` | Cursor position, column index preserved across reloads |

**Adaptation notes:**
- ticket's existing fsnotify watcher should remain as-is for local store mode
- Polling layer activates only when `store == "central"` in config
- Both layers emit the same `fileChangedMsg` so downstream handling is unified
- For cross-project inbox (tui-cross-project ticket), polling is the only option since we're watching multiple directories
