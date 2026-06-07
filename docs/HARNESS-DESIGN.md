# Harness — Native macOS Ticket + Worker App

> Design seed for a new project. Captures the motivation, goals, agreed
> architecture, and a v1 cut. Authored from a design conversation; intended to
> bootstrap a dedicated repo + ticket project, not to be built inside the `tk`
> repo.

## 1. Motivation

`tk` already provides solid ticket management — a CLI, a Bubbletea TUI (`tk ui`),
and an MCP server (`tk serve`) over a central multi-project store. What it does
**not** provide is a way to *run agents against tickets and supervise them*.

The gap surfaced concretely while building the `tk ui` `w` keybinding, which
spawns a Claude Code session for a ticket via `osascript`/iTerm:

- A one-shot terminal spawn can't be **monitored** — once launched, the TUI
  forgets the session. It can't tell whether the worker is running, finished,
  failed, or stuck waiting for input.
- Results only flow back **implicitly**, via the file watcher noticing the
  ticket changed on disk. There's no liveness, no progress, no failure signal.
- There's no way to **sequence** work across agents — e.g. implement with one
  agent, then review with another — because that requires orchestrating multiple
  distinct agent processes, which a single terminal spawn can't do.

A terminal UI and an `osascript` spawn have hit their ceiling. A native app can
own the worker processes, consume their structured event streams, and present a
real control plane.

## 2. What we're building

A **native macOS app whose priority is a harness for running agent workers
against tickets.** A ticket browser is the supporting surface the harness lives
in — not the headline feature.

Two halves, in priority order:

1. **Harness (priority).** Launch workers against tickets, watch them run, get
   results back. Support multi-stage, multi-agent **workflows** (e.g. execute
   with one agent, review with another).
2. **Ticket manager (supporting).** Browse/triage tickets across the central
   store, enough to pick what to launch against and see outcomes.

## 3. Goals

- **Replace the iTerm-spawn dead-end** with a supervised worker model: liveness,
  progress, and results visible in-app.
- **Autonomous, auto-advancing workflows** — point the harness at a ticket and
  let a multi-stage pipeline run to completion; you watch a board, not a token
  stream.
- **Interruptible where the backend allows** — attach to a live worker to
  answer/approve/nudge (full keyboard takeover is explicitly deferred; see §8).
- **Multi-agent** — support Claude Code, OpenAI Codex, and Cursor behind a
  uniform plugin model; a worker's backend is chosen per launch.
- **Reuse `tk`, don't reinvent it** — tickets are read/written through the
  existing `tk serve` MCP. One source of truth.

## 4. Non-goals (for the project as a whole, not just v1)

- Not a replacement for the agent CLIs themselves — it orchestrates them.
- Not a general CI/CD system — it runs agent workers against tickets, not
  arbitrary build pipelines.
- Not cross-platform in its first life — native macOS first. (Stack choice in §7
  may keep the door open, but portability is not a goal.)

## 5. Core architecture (agreed)

### 5.1 Plugin / adapter model per agent backend

There is **no single SDK** spanning Claude, Codex, and Cursor, so each backend is
a **plugin/adapter** behind a uniform contract. The harness core knows only the
contract; adapters encapsulate per-agent specifics.

**Capability contract:**

- `launch(ticket, workdir) -> worker` — universal.
- `stream() -> events` — universal. Structured events the core understands:
  at minimum `output`, `needs-input`, `done(result)`, `failed(err)`.
- `cancel()` — universal.
- `send(text)` — **optional capability flag.** Reply to a live worker
  (answer/approve/nudge). Adapters declare whether they support it; the app
  degrades gracefully when they don't.
- `attach()` — expose the live session for viewing (richer where supported).

### 5.2 Structured event streaming is the common substrate (not PTY)

Research (mid-2026) confirmed **all three CLIs have a headless mode that emits
structured, parseable events** — so the common substrate is JSON event streaming,
not terminal scraping:

| Backend | SDK | Headless CLI | Structured events | Send-to-live-session |
|---|---|---|---|---|
| **Claude Code** | Agent SDK (TS/Py) | `claude -p` | `--output-format stream-json` | **Yes** — streaming input, true mid-session injection + interrupt |
| **Codex** | `@openai/codex-sdk` (TS/Py) | `codex exec --json` | JSONL event stream | Partial — resume w/ new prompt; live attach via `codex app-server` (JSON-RPC/WebSocket) |
| **Cursor** | No first-party SDK | `cursor-agent -p` | `stream-json` NDJSON | **Local CLI: no** (one-shot). Cloud Agents REST API: yes (follow-up runs) |

**Implication for the contract:** `launch` + `stream` + `cancel` are universal;
**`send`/attach is a capability flag.** Claude fully exercises the
autonomous-*interruptible* design; Codex is resume-style; **local Cursor is
fire-and-forget.** The app must treat live-interrupt as optional, not assumed.

**Known integration caveats (verify at build time):**
- Claude Agent SDK: from 2026-06-15, SDK/`claude -p` usage on subscription plans
  draws from a separate Agent SDK credit pool; third parties **must use API-key
  auth** (no claude.ai login). Plan billing accordingly.
- Codex: mid-turn stdin injection into `codex exec --json` is unconfirmed; live
  control is the `app-server` lane's job.
- Cursor Cloud Agents API version (`/v0/` vs `/v1/`) and the CLI default
  `--output-format` are documented inconsistently — pin explicitly.

### 5.3 Workflows = state machines over the ticket

A **workflow** is an ordered set of **stages**; a stage = **(agent backend) ×
(role/skill)**. Stages **hand off through the ticket** — no bespoke
agent-to-agent message passing. Stage 1 writes to the ticket (status, notes,
branch/diff); stage 2 reads the ticket + branch and writes its result back. This
is exactly how the existing system already communicates.

Workflow types mirror the skills that already exist:

- `brainstorm → capture` (design, then create tickets)
- `work → review` (implement, then code-review)
- `investigate → fix → review` (bug)

**Stage gating: auto-advance.** Stage completes → harness auto-launches the next
→ to completion. The human watches the board; interrupts only when a worker
signals `needs-input` (or by choice, where the backend supports attach).

### 5.4 Isolation: git worktree per ticket / workflow-run

Auto-advancing, possibly-parallel workers must not clobber a shared working tree.
**Each ticket/workflow-run gets its own `git worktree`** on a branch named off the
ticket ID. Benefits:

- Cheap (no daemon), native to git, shared object store.
- The **branch + diff doubles as the handoff medium, the review artifact, and the
  merge unit** — stage 2 (review) reads stage 1's branch in the same worktree.
- Parallel workers on different tickets = different worktrees, no collisions.

**Limitation accepted for v1:** worktrees give *workspace* isolation, not
*security/blast-radius* isolation (same fs, same machine, no resource/network
sandbox). True sandboxing (containers) is deferred — see §8.

### 5.5 tk integration boundary

Tickets are read and written through the **existing `tk serve` MCP** (tools:
`ticket_list`, `ticket_show`, `ticket_edit`, `ticket_add_note`, `ticket_create`,
`ticket_dep`, etc.). The harness does **not** reimplement ticket storage/format
logic. Note `tk serve` currently exposes **no** run/stream tooling — the harness
half is genuinely greenfield and lives in the app, not in `tk`.

## 6. v1 scope — the spine

The riskiest, most novel piece is: *can the app reliably launch a real agent
against a worktree, stream structured events into a usable
running/done/failed/needs-input view, and capture the result back onto the
ticket?* Everything else is breadth on that spine. v1 proves the spine
end-to-end while exercising **every architectural seam** with the minimum
breadth.

**In scope:**
- **One backend** — a **Claude adapter**, built behind the plugin interface (the
  only adapter implemented, but the seam is real).
- **One workflow** — `work → review`, **both stages Claude** — proves cross-stage
  handoff via the ticket + auto-advance without needing a second backend yet.
- **One worktree per run**; review reads stage 1's branch.
- **Worker board** — launch against a ticket → per-stage
  running/done/failed/needs-input → result lands on the ticket (status, notes,
  branch).
- **Minimal ticket list** (from `tk serve` MCP) to choose a launch target.

**Explicitly NOT in v1:**
- Codex / Cursor adapters (second backend is the first real test of the
  abstraction — expect to adjust the interface then).
- Containers / blast-radius isolation.
- PTY keyboard-takeover (full interactive shell).
- brainstorm / investigate workflows.
- Multiple agents per stage / race-and-compare.
- Full ticket-editing UI.

v1 demonstrates every seam — adapter interface, event streaming, ticket-as-
handoff, auto-advance, worktrees — so adding a second backend or second workflow
is "fill in the seam," not "rearchitect."

## 7. Open decision: tech stack (for the design spike)

Deliberately **left open** — it's the first thing the design spike resolves,
because it reshapes the feature breakdown.

- **SwiftUI** — best native Mac feel and process control for owning worker
  processes; macOS-only.
- **Tauri** (Rust shell + web UI) — reuse web skills, easier future portability;
  less native, extra runtime.

Leaning SwiftUI given "native macOS" + heavy local process supervision, but the
spike should validate process management + event-stream consumption ergonomics in
whichever before committing.

## 8. Deferred to v2+

- **Codex and Cursor adapters** — second/third backends; "execute Claude / review
  Codex" is the headline cross-backend demo once the abstraction is proven.
- **Containers** as an opt-in isolation level (blast-radius containment for true
  unattended autonomy).
- **PTY keyboard-takeover** — full interactive shell attach, if the structured
  attach proves insufficient in real use.
- **brainstorm / investigate** workflows; **multi-agent-per-stage** (race/compare).
- Richer ticket-editing UI.

## 9. Proposed ticket structure (to seed the new project)

- **Epic:** Harness — native macOS ticket + worker app.
- **Design spike:** stack choice (SwiftUI vs Tauri) + adapter interface shape,
  validated on paper against all three CLIs. (Resolve before committing the
  feature breakdown — the stack choice changes what the children look like.)
- **v1 feature children** (each a vertical seam), pending the spike:
  1. Plugin/adapter interface + Claude adapter.
  2. Event-stream → worker-state model.
  3. Worktree-per-run runner.
  4. Ticket-as-handoff + auto-advance state machine (`work → review`).
  5. Worker board UI + ticket list (via `tk serve` MCP).
