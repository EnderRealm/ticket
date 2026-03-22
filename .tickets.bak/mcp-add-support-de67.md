---
id: mcp-add-support-de67
stage: done
status: open
risk: low
deps: []
links: []
created: 2026-03-01T00:55:12Z
type: feature
priority: 3
---
# MCP: Add support for creating tickets on a different repo in the create_ticket tool

Add a `repo` parameter to the `ticket_create` MCP tool that allows creating tickets in a different repo's `.tickets/` directory. Mirrors the CLI's `--repo` flag behavior (walks up from the given path to find `.tickets/`). Scoped to `ticket_create` only — other tools remain single-store.

## Acceptance Criteria

1. When `ticket_create` is called with a `repo` parameter (absolute or relative directory path), the ticket is created in the `.tickets/` directory found by walking up from that path. The path is resolved to absolute before walking. Same semantics as CLI `--repo` flag.
2. When `ticket_create` is called without `repo` (or with empty string), behavior is unchanged — uses the server's default store.
3. When `repo` is provided but no `.tickets/` directory is found walking up from that path, the tool returns `IsError=true` with text containing "no .tickets/ directory found".
4. The `repo` field on `createArgs` has a `jsonschema` struct tag describing the behavior (path to repo root, walks up to find .tickets/).
5. New test: creates a ticket via `repo` pointing to a separate temp directory with `.tickets/` pre-created. Asserts ticket lands in the alternate directory and the server's default store is unaffected.

**Out of scope:** Cross-store `parent` validation. If `parent` references a ticket in a different store, no cross-store resolution is attempted.

**Design constraint:** The `findTicketsDir` walk-up logic must be shared between CLI and MCP via `pkg/ticket/`. `cmd/root.go` is refactored to call the shared version.

## Design

### Change 1: Export `FindTicketsDir` in `pkg/ticket/store.go`

Add exported function. Takes an **absolute** path as precondition — callers must resolve with `filepath.Abs` before calling.

```go
func FindTicketsDir(startDir string) (string, bool) {
    dir := startDir
    for {
        candidate := filepath.Join(dir, ".tickets")
        if info, err := os.Stat(candidate); err == nil && info.IsDir() {
            return candidate, true
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            break
        }
        dir = parent
    }
    return "", false
}
```

### Change 2: Refactor `cmd/root.go`

Remove local `findTicketsDir` (line 179). Replace call sites in `TicketsDir()` with `ticket.FindTicketsDir`. `filepath.Abs` remains in `TicketsDir()` — not moved into the shared function.

### Change 3: Add `Repo` field to `createArgs` in `internal/mcp/mcp.go`

```go
Repo string `json:\"repo,omitempty\" jsonschema:\"path to repo root; walks up to find .tickets/ directory (like CLI --repo flag). Relative paths resolve against server CWD.\"`
```

### Change 4: Handle `repo` in `registerCreate` handler

After title validation (line 375), before ticket construction:

```go
targetStore := store
if args.Repo != \"\" {
    abs, err := filepath.Abs(args.Repo)
    if err != nil {
        r, _ := errResult(\"invalid repo path: %v\", err)
        return r, nil, nil
    }
    dir, ok := ticket.FindTicketsDir(abs)
    if !ok {
        r, _ := errResult(\"no .tickets/ directory found under %s\", abs)
        return r, nil, nil
    }
    targetStore = ticket.NewFileStore(dir)
}
```

Use `targetStore.Create(t)` instead of `store.Create(t)` at line 431.

Also update tool description (line 373) to: `\"Create a new ticket. Supports optional repo parameter for cross-repo creation.\"`

### Change 5: Test in `internal/mcp/mcp_test.go`

`TestCreateTicketRemoteRepo`:
1. `session := testServer(t)` — gets server with default temp dir
2. `altDir := t.TempDir()` then `os.MkdirAll(filepath.Join(altDir, ".tickets"), 0o755)` — create alternate repo with `.tickets/`
3. Call `ticket_create` with `repo: altDir`, assert `result.IsError` is false
4. Read files in `altDir/.tickets/` — assert exactly one `.md` file exists
5. Call `ticket_create` without `repo` — assert ticket lands in server default dir (list default dir)
6. Test error case: call with `repo` pointing to a dir with no `.tickets/` — assert `result.IsError` is true and content contains "no .tickets/ directory found"

### Files Changed

| File | Change |
|------|--------|
| `pkg/ticket/store.go` | Add `FindTicketsDir` |
| `cmd/root.go` | Replace local `findTicketsDir` with `ticket.FindTicketsDir` |
| `internal/mcp/mcp.go` | Add `Repo` to `createArgs`, handle in handler, update tool description |
| `internal/mcp/mcp_test.go` | Add `TestCreateTicketRemoteRepo` |

## Design

### Change 1: Export `FindTicketsDir` in `pkg/ticket/store.go`

Add a new exported function:

```go
// FindTicketsDir walks up from startDir looking for a .tickets/ subdirectory.
// Returns the path and true if found, or empty string and false.
func FindTicketsDir(startDir string) (string, bool) {
    dir := startDir
    for {
        candidate := filepath.Join(dir, ".tickets")
        if info, err := os.Stat(candidate); err == nil && info.IsDir() {
            return candidate, true
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            break
        }
        dir = parent
    }
    return "", false
}
```

### Change 2: Refactor `cmd/root.go` to call shared function

Replace `findTicketsDir` body with a call to `ticket.FindTicketsDir`. Remove the local `findTicketsDir` function.

### Change 3: Add `Repo` field to `createArgs` in `internal/mcp/mcp.go`

```go
type createArgs struct {
    // ... existing fields ...
    Repo string `json:"repo,omitempty" jsonschema:"path to repo root; walks up to find .tickets/ directory (like CLI --repo flag)"`
}
```

### Change 4: Handle `repo` in `registerCreate` handler

After title validation, before creating the ticket:

```go
targetStore := store
if args.Repo != "" {
    abs, err := filepath.Abs(args.Repo)
    if err != nil {
        r, _ := errResult("invalid repo path: %v", err)
        return r, nil, nil
    }
    dir, ok := ticket.FindTicketsDir(abs)
    if !ok {
        r, _ := errResult("no .tickets/ directory found under %s", abs)
        return r, nil, nil
    }
    targetStore = ticket.NewFileStore(dir)
}
```

Then use `targetStore.Create(t)` instead of `store.Create(t)`.

### Change 5: Test in `internal/mcp/mcp_test.go`

New test `TestCreateTicketRemoteRepo`:
1. Create two temp dirs: server default (via `testServer`) and a "remote" dir with `.tickets/` pre-created.
2. Call `ticket_create` with `repo` pointing to the remote dir.
3. Assert ticket file exists in remote `.tickets/`, not in server default dir.
4. Call `ticket_create` without `repo`, assert it lands in server default dir.

### Files Changed

| File | Change |
|------|--------|
| `pkg/ticket/store.go` | Add `FindTicketsDir` |
| `cmd/root.go` | Refactor to use `ticket.FindTicketsDir` |
| `internal/mcp/mcp.go` | Add `Repo` to `createArgs`, use it in handler |
| `internal/mcp/mcp_test.go` | Add `TestCreateTicketRemoteRepo` |

## Test Results

```\n$ go test ./...\n?   	github.com/EnderRealm/ticket	[no test files]\n?   	github.com/EnderRealm/ticket/cmd	[no test files]\nok  	github.com/EnderRealm/ticket/internal/mcp	0.336s\nok  	github.com/EnderRealm/ticket/internal/tui	(cached)\nok  	github.com/EnderRealm/ticket/pkg/ticket	0.519s\n```\n\nAll tests pass including new tests:\n- TestCreateTicketRemoteRepo: ticket created in alternate repo, default store unaffected\n- TestCreateTicketRemoteRepoNotFound: returns IsError with correct message when .tickets/ not found

## Review Log

**2026-03-12T05:48:21Z [agent:spec-builder]**
APPROVED — Criteria revised based on review: tightened error shape (AC3), specified path resolution (AC1), added empty-string handling (AC2), made test assertions specific (AC5), moved shared-code constraint out of AC into design constraint, documented cross-store parent as out-of-scope.

**2026-03-12T05:51:19Z [agent:design-reviewer]**
APPROVED — Design revised to address findings: FindTicketsDir takes pre-resolved absolute path (callers handle filepath.Abs), test explicitly creates .tickets/ via os.MkdirAll, checks result.IsError not err, tool description updated to mention repo param, error message matches AC3 exactly. All file paths and line numbers verified against codebase.

**2026-03-12T05:55:04Z [agent:code-reviewer]**
APPROVED — Clean, focused change. FindTicketsDir correctly abstracted to pkg/ticket, targetStore pattern keeps default path untouched, no security concerns. Minor suggestions: tool description could be more explicit, FindTicketsDir comment-only contract on absolute path. None blocking.

**2026-03-12T05:55:05Z [agent:impl-reviewer]**
APPROVED — All 5 acceptance criteria satisfied with direct code evidence. Design constraint fully executed — cmd/root.go refactored, no duplicate walk logic. No scope creep, no TODOs, no missing exports.

**2026-03-12T06:38:57Z [human:steve]**
APPROVED — All AC verified.

## Notes

**2026-03-12T05:45:27Z**

**Decision:** (human) Scope to `ticket_create` only, use `repo` param with walk-up semantics matching CLI `--repo` flag. Can extend to other tools later.
