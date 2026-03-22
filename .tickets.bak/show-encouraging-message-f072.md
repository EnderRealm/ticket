---
id: show-encouraging-message-f072
stage: done
status: open
deps: []
links: []
created: 2026-02-27T07:08:22Z
type: feature
priority: 3
assignee: Steve Macbeth
tags: [ux]
---
# Show encouraging message on empty listing output

When any listing command (ls, ready, blocked, inbox, closed, pipeline, next) returns zero results, display a random encouraging message instead of empty output. Rotate between ~20 messages using a shared function.

## Acceptance Criteria

1. When any listing command (ls, ready, blocked, inbox, closed, pipeline, next) returns zero results, the system shall display a random encouraging message from a pool of ~20 messages, verified by running tk ls in an empty project and confirming a message appears.
2. When the --json flag is set on a listing command with zero results, the system shall output an empty JSON array [] instead of the encouraging message, verified by running tk ls --json in an empty project and confirming valid JSON output.
3. When a listing command returns zero results due to filters, the system shall show the same encouraging message as a fully empty project, verified by running tk ls --tag nonexistent and confirming an encouraging message appears.

## Design

New file `cmd/messages.txt` with ~20 encouraging messages, one per line. New file `cmd/empty.go` with `printEmptyMessage()` that uses `go:embed` to load messages.txt, splits on newlines, picks random via `math/rand`. When `jsonOutput` is true, prints `[]` instead. Each listing command (ls, ready, blocked, inbox, closed, pipeline, next) calls `printEmptyMessage()` when results are empty, replacing existing messages in inbox and next.

**Files:**
- `cmd/messages.txt` — new, message pool
- `cmd/empty.go` — new, `printEmptyMessage()`
- `cmd/ls.go` — empty check
- `cmd/ready.go` — empty check
- `cmd/blocked.go` — empty check
- `cmd/inbox.go` — replace existing message
- `cmd/closed.go` — empty check
- `cmd/pipeline.go` — empty check
- `cmd/next.go` — replace existing message

## Test Results

- [x] AC1: ls, ready, blocked, closed, pipeline all show encouraging message on empty results
- [x] AC2: ls --json outputs [] on empty results
- [x] AC3: filtered-empty produces same behavior as fully empty
- [x] go test ./... passes

## Review Log

**2026-02-27T07:21:07Z [human:steve]**
APPROVED — Acceptance criteria confirmed through conversation

**2026-02-27T07:23:48Z [human:steve]**
APPROVED — Design approved: embedded messages.txt, shared printEmptyMessage(), all 7 listing commands

**2026-02-27T07:30:37Z [agent:code-review]**
APPROVED — Approved. Empty guard added per suggestion, pipeline check simplified.

**2026-02-27T07:30:40Z [agent:impl-review]**
APPROVED — All 3 acceptance criteria satisfied, design adhered to, no scope creep.

**2026-02-27T07:36:46Z [human:steve]**
APPROVED — Verified manually, messages land well

## Notes

**2026-02-27T07:16:48Z**

## Triage

**Risk:** low — purely additive output change, no external deps, no API changes

**Scope:** single task

**2026-02-27T07:16:48Z**

## Triage

**Risk:** low — purely additive output change, no external deps, no API changes

**2026-02-27T07:16:48Z**

## Triage

**2026-02-27T07:16:48Z**

## Triage**Risk:** low — purely additive output change, no external deps, no API changes**Scope:** single task**Key decisions:**
- All listing commands, not just ls (human)
- ~20 rotating messages via shared function (human)

**2026-02-27T07:19:54Z**

## Spec

**2026-02-27T07:19:54Z**

## Spec**Scope:**
- In: ls, ready, blocked, inbox, closed, pipeline, next — all get consistent empty-state behavior
- In: Replace existing messages in inbox ("Inbox empty") and next ("No active projects")
- In: --json returns [] on empty
- Out: No differentiation between filtered-empty and project-empty

**2026-02-27T07:19:54Z**

## Spec**Scope:**
- In: ls, ready, blocked, inbox, closed, pipeline, next — all get consistent empty-state behavior
- In: Replace existing messages in inbox ("Inbox empty") and next ("No active projects")
- In: --json returns [] on empty
- Out: No differentiation between filtered-empty and project-empty**Decisions:**
- All listing commands, consistent behavior (human)
- Replace inbox/next existing messages too (human)
- Encouraging message for all empty cases regardless of filters (human)
- --json outputs [] not a string (human)
