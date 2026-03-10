# Agentic Workflow Redesign Proposal

## Executive Summary

Your current system is four repos doing overlapping, loosely-coupled jobs:

| Repo | What it does today |
|------|-------------------|
| **ticket** (`tk`) | Ticket CRUD, status lifecycle, dependency DAG, MCP server |
| **powers** | Claude Code skills (slash commands), hooks, agent definitions |
| **manager** | Web dashboard, kanban board, agent runner, service management, nightly pipelines |
| **learnings** (ghost-data) | Passive storage for session summaries, patterns, rollups |

The fundamental limitation: **the workflow is encoded in prompt text (skills), not in the ticket system itself.** The `/create-feature` skill describes an 8-phase workflow, but `tk` only knows 4 statuses: `open → in_progress → needs_testing → closed`. There's no structural enforcement of your design-review-implement-test pipeline. The skill *asks* Claude to follow the process; nothing *prevents* skipping steps.

This proposal redesigns the system so the workflow is **structurally enforced** — the ticket system itself becomes the state machine, and skills/agents become the actors that advance tickets through gates.

---

## Part 1: The Problem Diagnosis

### What's working

1. **Markdown + YAML tickets in git** — This is a genuinely good design. Human-readable, AI-readable, diffable, mergeable, no server needed. Keep it.
2. **Dependency DAG with readiness gating** — The `ready`/`blocked` concepts are sound. The parent-child hierarchy with status propagation is elegant.
3. **Powers/skills as prompt engineering** — The idea of encoding workflows as markdown instructions is powerful and maintainable.
4. **Learning extraction pipeline** — The PreCompact → SessionEnd → nightly extraction → pattern detection pipeline is sophisticated and well-designed.
5. **Manager dashboard** — The kanban board, agent runner, and SSE-based real-time updates are solid infrastructure.

### What's broken

1. **Workflow lives in the wrong place.** The `/create-feature` skill describes 8 phases (brainstorm → create ticket → plan → validate → execute → test → finalize → commit), but `tk` has 4 statuses. There's no way to ask "show me all tickets awaiting design review" or "what's blocked on human approval." The workflow is invisible to the system.

2. **No quality gates.** Nothing prevents an agent from skipping from "open" to "closed." The `needs_testing` status is the only gate, and it's optional. There's no design review gate, no code review gate, no test verification gate.

3. **No concept of review.** Tickets move through statuses, but there's no way to express "this ticket's design needs human review" vs "this ticket's implementation needs AI review" vs "this code needs human code review." These are fundamentally different checkpoints requiring different actors.

4. **Skills duplicate what should be ticket state.** The `/create-feature` skill uses HTML comments (`<!-- checkpoint: planning -->`) embedded in ticket bodies to track workflow position. This is invisible to `tk ls`, `tk ready`, or any query. It's a shadow state system.

5. **Four repos with tangled responsibilities.** Powers defines workflows. Manager runs agents and manages services. Ticket tracks work. Learnings stores knowledge. But: powers has its own `.tickets/`, manager has its own ticket wrapper, and the learning extraction pipeline spans all three. Changes to the workflow touch 3 repos.

6. **Agent runner is too simple.** The manager's agent runner formats a ticket into a prompt and launches Claude. But it doesn't enforce which phase the agent should work on, doesn't verify the agent did what was asked, doesn't run the review agents, and doesn't advance the ticket through gates.

7. **No conversation tracking.** Your requirement says some phases "may or may not be conversational." But there's no way to track that a brainstorming conversation happened, what decisions were made in it, or link it to the resulting ticket.

8. **Learning feedback loop is broken.** Session summaries are extracted and stored, but they don't flow back into the system effectively. The nightly pipeline creates tickets for discoveries, but those tickets enter the same undifferentiated backlog as everything else.

---

## Part 2: The Redesigned Workflow

### The 7-Stage Pipeline

```
┌─────────────┐   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐
│   TRIAGE     │──▸│   SPEC       │──▸│   DESIGN     │──▸│   IMPLEMENT  │
│              │   │              │   │              │   │              │
│ Idea capture │   │ AI builds    │   │ AI writes    │   │ AI codes     │
│ Human/AI     │   │ the ticket   │   │ the design   │   │              │
│ conversatn'l │   │ Human review │   │ Agent review │   │ Agent review │
│              │   │              │   │ Human review │   │ Code review  │
└─────────────┘   └─────────────┘   └─────────────┘   └─────────────┘
                                                              │
                                                              ▾
                  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐
                  │   DONE       │◂──│   VERIFY     │◂──│   TEST       │
                  │              │   │              │   │              │
                  │ Merged to    │   │ Human        │   │ Agent runs   │
                  │ main         │   │ acceptance   │   │ tests        │
                  │              │   │ testing      │   │ Human tests  │
                  └─────────────┘   └─────────────┘   └─────────────┘
```

### Stage Definitions (Full Pipeline — Features)

| Stage | Entry Gate | Who Acts | Exit Gate | Artifacts |
|-------|-----------|----------|-----------|-----------|
| **triage** | None (new idea) | Human or AI | Human marks "ready to spec" | Title, rough description, type, priority |
| **spec** | Triage approved | AI builds ticket, human reviews | Human approves spec | Full description, acceptance criteria (EARS format), scope, context |
| **design** | Spec approved | AI writes design, design-review agent validates (advisory), human reviews | Human approves design | Design section, implementation plan, file list, test plan |
| **implement** | Design approved | AI implements, impl-review agent verifies completeness (mandatory), code-review agent reviews (mandatory) | All reviews pass | Code changes, PR/branch |
| **test** | Implementation reviewed | AI runs tests, human runs manual tests | Tests pass | Test results, coverage |
| **verify** | Tests pass | Human acceptance testing | Human approves | Verification notes |
| **done** | Verified | Auto (merge + close) | N/A | Merged code |

### Type-Dependent Pipelines

Not every ticket needs the full pipeline. The stages a ticket passes through depend on its type:

```
feature:  triage → spec → design → implement → test → verify → done  (7 stages)
bug:      triage → implement → test → verify → done                   (5 stages)
chore:    triage → implement → done                                    (3 stages)
epic:     triage → spec → design → done                                (4 stages)
```

The `tk advance` command knows each type's pipeline and advances to the correct next stage. Skip (`tk skip`) remains available for edge cases — a "bug" that actually needs design work, or a "chore" that should be tested.

### Key Design Decisions

**1. Stages vs. statuses.** The current system uses "status" as a flat enum. The redesign uses "stage" — a position in a pipeline. Every ticket moves forward through stages. The stage tells you exactly what needs to happen next and who needs to do it.

**2. Review is a sub-state, not a stage.** Within each stage, work can be `active` (being worked on) or `review` (awaiting review). This avoids stage proliferation while still tracking review state. Agent reviews are mandatory gates at `implement → test`; advisory-but-visible at all other transitions.

**3. Gates are structural and type-aware.** `tk` itself enforces stage ordering based on ticket type. A feature can't skip from `spec` to `implement` without `design`. A bug goes straight from `triage` to `implement`. The tool enforces it — skills don't have to remember.

**4. Agents are actors, not workflows.** Instead of one monolithic `/create-feature` skill that does everything, each stage has focused agents:
- **Spec agent**: Takes triage info, builds structured ticket
- **Design agent**: Takes spec, writes implementation plan
- **Design review agent**: Validates design against codebase (your existing plan-reviewer)
- **Implement agent**: Takes design, writes code
- **Impl review agent**: Verifies implementation matches design and acceptance criteria
- **Code review agent**: Reviews code quality, patterns, security
- **Test agent**: Runs tests, verifies coverage

**5. Conversations are first-class.** Some stages (triage, spec) may involve back-and-forth. The system tracks conversation sessions linked to tickets via session IDs *and* preserves focused decision summaries inline in the ticket's Notes section. Session links answer "what happened?"; inline summaries answer "what was decided?"

**6. Stage skipping is explicit.** Small bug fixes don't need a full design phase. The system supports explicit stage skipping (`tk advance <id> --skip-to implement --reason "trivial fix"`) with an audit trail — but skipping is a conscious act, not a default.

### New Ticket Schema

```yaml
---
id: t-5a3f
stage: design          # Pipeline position (replaces "status")
review: pending        # null | pending | approved | rejected
type: feature
priority: 1
assignee: Steve
parent: t-epic1
deps: [t-abc1]
links: [t-xyz1]
tags: [auth, api]
created: 2026-02-25T10:00:00Z
conversations: [sess-abc123, sess-def456]   # Linked Claude sessions
skipped: []            # Stages explicitly skipped
blocked_reason: null   # Why this is blocked (human-readable)
external-ref: gh-123
---
# Login Flow

## Description
...

## Acceptance Criteria
- WHEN a user submits valid credentials THE system SHALL return a JWT token
- WHEN a user submits invalid credentials THE system SHALL return 401
- IF the account is locked THE system SHALL NOT attempt authentication

## Design
...

## Implementation Plan
1. Create auth middleware in `src/middleware/auth.ts`
2. Add login endpoint in `src/routes/auth.ts`
3. Write tests in `tests/auth.test.ts`

## Test Plan
- Unit: auth middleware with valid/invalid/expired tokens
- Integration: login endpoint with fixtures
- Edge: locked account, rate limiting

## Review Log
**2026-02-25T12:00:00Z [design-review-agent]**
READY — All file paths verified, API patterns consistent with codebase.

**2026-02-25T14:00:00Z [human:steve]**
Approved. Proceed to implementation.

## Notes
**2026-02-25T10:30:00Z**
Decided on JWT over session cookies for API-first approach.
```

### What Changes in `tk`

**New commands:**
- `tk advance <id> [--to <stage>]` — Move ticket to next stage (enforces gate checks)
- `tk review <id> --approve|--reject [--comment "..."]` — Record review decision
- `tk skip <id> --to <stage> --reason "..."` — Skip stages with audit trail
- `tk log <id>` — Show full stage transition history
- `tk pipeline [--stage <stage>]` — Show tickets grouped by pipeline stage

**Modified commands:**
- `tk ready` — Now stage-aware: shows tickets ready for *their current stage's* next action
- `tk ls` — Default grouping becomes pipeline view (triage → spec → ... → done)
- `tk create` — New tickets start at `triage` stage
- `tk show` — Shows stage, review status, transition history, linked conversations

**Removed concepts:**
- `status` field → replaced by `stage`
- `needs_testing` status → replaced by `test` stage
- `in_progress` status → implied by being in any active stage

**Stage transition rules (enforced by `tk advance`):**
```
triage   → spec       (requires: description exists)                              [feature, epic]
triage   → implement  (requires: description exists)                              [bug, chore]
spec     → design     (requires: acceptance criteria exist, review=approved)       [feature, epic]
design   → implement  (requires: design + plan exist, review=approved)             [feature]
design   → done       (requires: design exists, review=approved)                   [epic]
implement→ test       (requires: code-review=approved, impl-review=approved)       [feature, bug] MANDATORY
implement→ done       (requires: advisory review surfaced)                         [chore]
test     → verify     (requires: test results recorded, all pass)                  [feature, bug]
verify   → done       (requires: review=approved)                                  [feature, bug]
```

---

## Part 3: Repo Consolidation

### The Problem

Four repos with overlapping concerns:

```
Currently:
  ticket    ←→  powers    ←→  manager    ←→  learnings
  (CLI)         (skills)      (dashboard)     (data)
```

The workflow logic is split across all of them. A change to the pipeline touches `ticket` (stages), `powers` (skills), and `manager` (agent runner). The learnings pipeline spans `powers` (hooks), `manager` (scripts), and `learnings` (storage).

### Proposal: Two Repos

**Repo 1: `tk`** (the engine)
- The CLI tool (Go binary)
- The MCP server
- Stage pipeline enforcement
- Review system
- Gate validation logic
- The TUI

**Repo 2: `forge`** (the workshop)
- Everything that orchestrates work *using* `tk`
- Powers/skills (Claude Code plugin)
- Agent definitions (spec-agent, design-agent, review agents, etc.)
- Manager dashboard (web UI + API)
- Learning extraction pipeline (hooks, scripts, nightly jobs)
- Learnings data store (sessions, patterns, rollups)
- Service management

### Why "forge"?

The metaphor: `tk` is the raw material (tickets). `forge` is where you shape them — the workshop with tools (skills), workers (agents), a control panel (dashboard), and institutional memory (learnings). The name is short, memorable, and suggests creation.

### Why two, not one?

`tk` remains a standalone tool usable by anyone — it's a general-purpose ticket system. `forge` is *your* opinionated workflow built on top of it. Other people could build different workflows on `tk`. The separation also means `tk` can be distributed via Homebrew/AUR without dragging in your personal workflow tooling.

### Why not keep four?

The current 4-repo split creates too many coordination points. Every workflow change touches 3 repos. The learnings pipeline alone spans powers (hooks), manager (scripts), and learnings (storage). Consolidating into `forge` means one repo, one commit, one deploy for workflow changes.

### Migration Path

```
ticket   → tk          (rename, same repo, same binary)
powers   → forge/      (skills/, hooks/, agents/ move in)
manager  → forge/      (src/server/, src/client/ move in)
learnings→ forge/data/ (sessions/, patterns/, rollups/ move in)
```

The `forge` repo structure:

```
forge/
├── .claude-plugin/          # Plugin metadata
├── skills/                  # Claude Code skills (from powers)
│   ├── triage/SKILL.md      # /triage - idea capture
│   ├── spec/SKILL.md        # /spec - build ticket spec
│   ├── design/SKILL.md      # /design - write implementation plan
│   ├── implement/SKILL.md   # /implement - write code
│   ├── review/SKILL.md      # /review - trigger review agents
│   ├── test-ticket/SKILL.md # /test-ticket - run tests
│   ├── investigate/SKILL.md # /investigate - debugging (unchanged)
│   ├── brainstorm/SKILL.md  # /brainstorm - design sessions (unchanged)
│   └── using-forge/SKILL.md # Session start context
├── agents/                  # Agent definitions
│   ├── spec-builder.md      # Builds structured specs from triage
│   ├── design-reviewer.md   # Validates designs against codebase
│   ├── impl-reviewer.md     # Verifies implementation completeness
│   ├── code-reviewer.md     # Code quality, patterns, security
│   └── test-runner.md       # Runs and validates tests
├── hooks/                   # Claude Code hooks (from powers)
├── src/
│   ├── server/              # API + agent orchestrator (from manager)
│   └── client/              # Web dashboard (from manager)
├── scripts/                 # Nightly pipelines (from manager)
├── data/                    # Learnings store (from learnings)
│   ├── sessions/
│   ├── patterns/
│   └── rollups/
├── templates/               # Project templates
├── docs/                    # Conventions, architecture
└── CLAUDE.md
```

---

## Part 4: The Agent Architecture

### Agent Roles

Each agent is a focused specialist, defined as a markdown file with constrained tools and a clear mandate.

```
┌──────────────────────────────────────────────────────────┐
│                    HUMAN (you)                            │
│  Approves specs, designs, and final verification         │
│  Triages ideas, sets priorities                          │
│  Makes architectural decisions                           │
└──────────┬────────────────────────────────┬───────────────┘
           │                                │
           ▾                                ▾
┌────────────────────┐           ┌────────────────────────┐
│   Spec Builder      │           │   Implement Agent      │
│   Agent             │           │                        │
│                     │           │   Writes code          │
│   Takes triage      │           │   following the        │
│   info, builds      │           │   approved design      │
│   structured spec   │           │   and plan             │
│   with EARS         │           │                        │
│   criteria          │           │   Tools: Read, Write,  │
│                     │           │   Edit, Bash, Glob,    │
│   Tools: Read,      │           │   Grep                 │
│   Glob, Grep,       │           │                        │
│   WebSearch         │           │                        │
└────────────────────┘           └────────────────────────┘
           │                                │
           ▾                                ▾
┌────────────────────┐           ┌────────────────────────┐
│   Design Agent      │           │   Impl Review Agent    │
│                     │           │                        │
│   Writes impl       │           │   Checks: all          │
│   plan, identifies  │           │   acceptance criteria  │
│   files, test       │           │   met, design          │
│   strategy          │           │   followed, no         │
│                     │           │   scope creep          │
│   Tools: Read,      │           │                        │
│   Glob, Grep        │           │   Tools: Read, Glob,   │
└────────────────────┘           │   Grep                  │
           │                     └────────────────────────┘
           ▾                                │
┌────────────────────┐                      ▾
│   Design Review     │           ┌────────────────────────┐
│   Agent             │           │   Code Review Agent    │
│                     │           │                        │
│   Validates:        │           │   Checks: style,       │
│   - File paths      │           │   patterns, security,  │
│   - API existence   │           │   maintainability,     │
│   - Pattern          │           │   test coverage        │
│     consistency     │           │                        │
│                     │           │   Tools: Read, Glob,   │
│   Tools: Read,      │           │   Grep                 │
│   Glob, Grep        │           └────────────────────────┘
│                     │                     │
│   Verdict: READY    │                     ▾
│   or REVISE         │           ┌────────────────────────┐
└────────────────────┘           │   Test Agent            │
                                  │                        │
                                  │   Runs test suite      │
                                  │   Verifies coverage    │
                                  │   Reports results      │
                                  │                        │
                                  │   Tools: Read, Bash,   │
                                  │   Glob, Grep           │
                                  └────────────────────────┘
```

### Agent Orchestration

The manager's agent runner evolves from "format ticket, launch Claude" to a **stage-aware orchestrator**:

```
User clicks "Advance" on ticket t-5a3f (currently at stage: spec, review: approved)
  │
  ├── Orchestrator checks gate: spec approved? ✓
  ├── Orchestrator advances ticket: tk advance t-5a3f --to design
  ├── Orchestrator launches Design Agent against t-5a3f
  │     └── Agent reads ticket, writes design + impl plan
  │     └── Agent runs: tk edit t-5a3f --design "..." --plan "..."
  │
  ├── Orchestrator launches Design Review Agent
  │     └── Agent validates design against codebase
  │     └── Agent records verdict: tk review t-5a3f --approve
  │     └── OR: tk review t-5a3f --reject --comment "file X doesn't exist"
  │
  ├── If rejected: notify human, ticket stays at design stage
  ├── If approved: notify human for final design approval
  │
  └── Human approves → orchestrator can advance to implement
```

This is the **evaluator-optimizer pattern** from Anthropic's research: one agent generates, another evaluates, iterate until quality gate passes.

### The Key Insight: Agents Don't Control Flow

In the current system, the `/create-feature` skill is a monolithic 8-phase workflow where Claude tracks its own progress via checkpoint comments. This is fragile — Claude can lose track, skip steps, or get confused after context compaction.

In the redesign, **`tk` controls flow. Agents just do work.** Each agent:
1. Reads the ticket
2. Does its focused job
3. Updates the ticket
4. Returns

The orchestrator decides what to run next based on ticket state. If an agent crashes, the ticket is still at the same stage — just re-run the agent. No checkpoint comments, no shadow state.

---

## Part 5: Conversational Phases

### The Problem

Your requirement: "Idea generation... may or may not be conversational." And: "AI builds out the ticket and leaves it for human review, which should be conversational."

The current system has no concept of conversations linked to tickets. Decisions made during brainstorming vanish when the session ends (unless manually captured in notes).

### The Solution: Session Links + Inline Summaries

Tickets gain a `conversations` field — an array of session IDs linking to Claude Code sessions where this ticket was discussed.

```yaml
conversations: [sess-abc123, sess-def456]
```

When a skill runs in conversational mode, it:
1. Records the session ID on the ticket: `tk edit t-5a3f --add-conversation $SESSION_ID`
2. The session summary (via the existing PreCompact hook) captures the full conversation context
3. At the end of the conversational phase, the agent writes a focused decision summary (3-5 bullets) into the ticket's `## Notes` section
4. The nightly pipeline links session summaries to tickets for cross-referencing

The session link answers "what happened?" — the full decision trail if you ever need to understand why. The inline summary answers "what was decided?" — the quick reference that downstream agents and future-you actually need.

### Conversational vs. Autonomous Mode

Each stage can run in either mode:

| Stage | Default Mode | Override |
|-------|-------------|----------|
| triage | Conversational | — |
| spec | Conversational | `--auto` for small bugs |
| design | Autonomous (agent writes) + Conversational (human reviews) | — |
| implement | Autonomous | `--interactive` for pairing |
| test | Autonomous | — |
| verify | Conversational | — |
| done | Automatic | — |

The `--auto` flag on skills controls this, as it does today. The key change is that the *default* mode is explicit per stage, not a global toggle.

---

## Part 6: Learning System Redesign

### Current State

The learning pipeline is sophisticated but disconnected:
1. PreCompact hook generates session summary (good)
2. SessionEnd hook extracts to learnings repo (good)
3. Nightly pipeline creates tickets from discoveries (good in theory)
4. But: created tickets enter an undifferentiated backlog
5. And: learnings rarely flow back into CLAUDE.md or skills

### Proposed Changes

**1. Learnings become ticket-linked, not session-linked.**

Currently: `sessions/project-name/2026-02-25-session.md` — organized by project and date.

Proposed: Session summaries are still stored by date, but decisions and learnings are automatically linked to the tickets they relate to. The nightly pipeline writes them into the ticket's `## Notes` section with proper attribution.

**2. Pattern detection creates actionable items, not just observations.**

Currently: Patterns are stored as `ptr-NNN.md` files in the learnings repo. Someone has to manually check them.

Proposed: When a pattern graduates from "observation" to "pattern" (3+ occurrences), it automatically:
- Creates a ticket in `forge` (type: `chore`, tag: `pattern-action`)
- If it's a coding pattern → suggests a CLAUDE.md addition
- If it's a workflow pattern → suggests a skill modification
- If it's a failure pattern → suggests a test or lint rule

**3. Rollups feed the dashboard.**

The daily/weekly/monthly rollups already exist. The manager dashboard should surface them prominently — not buried in an "Insights" tab. The dashboard home should show:
- This week's velocity (tickets completed)
- Active patterns requiring attention
- Recent learnings relevant to in-progress tickets

**4. The data stays in `forge/data/`.**

No separate learnings repo. Session summaries, patterns, and rollups live alongside the skills and dashboard code. One repo, one git history, one deploy.

---

## Part 7: Implementation Roadmap

### Phase 1: Ticket Pipeline (in `tk`)

**Changes to the Go binary:**
1. Add `stage` field (replaces `status`, with migration for existing tickets)
2. Add `review` field
3. Add `conversations` field
4. Add `skipped` field
5. Implement `tk advance` with gate enforcement
6. Implement `tk review`
7. Implement `tk skip`
8. Implement `tk log` (transition history)
9. Implement `tk pipeline` (pipeline view)
10. Update `tk ready` for stage awareness
11. Update `tk ls` default grouping
12. Update MCP server with new commands
13. Update TUI with pipeline view
14. Backward compatibility: treat old `status` values as stage equivalents during migration

**Migration mapping:**
```
open         → triage
in_progress  → implement  (best guess — may need manual triage)
needs_testing→ test
closed       → done
```

### Phase 2: Agent Definitions (in `forge`)

1. Write focused agent definitions for each role
2. Refactor `/create-feature` into stage-specific skills
3. Update hooks for the new pipeline
4. Implement the stage-aware orchestrator in the manager

### Phase 3: Dashboard Updates (in `forge`)

1. Replace kanban columns with pipeline stages
2. Add review approval UI
3. Add conversation linking
4. Surface learnings on dashboard home
5. Add stage transition controls

### Phase 4: Learning Integration

1. Move learnings data into `forge/data/`
2. Update nightly pipelines for ticket-linked learnings
3. Implement pattern → actionable item pipeline
4. Add rollup display to dashboard

---

## Part 8: What This Enables

### Today's workflow (manual, error-prone)
```
You: /create-feature "login flow"
Claude: [runs through 8 phases, tracking progress via HTML comments]
Claude: [might skip steps, lose context after compaction, forget to test]
You: [manually verify everything happened]
```

### Redesigned workflow (structural, verifiable)
```
You: /triage "users need to log in"
Claude: [creates ticket at triage stage, asks clarifying questions]
You: "JWT-based, API-first"
You: tk advance t-5a3f  (or click in dashboard)

→ Spec agent runs, builds structured ticket with EARS criteria
→ You review spec in dashboard, approve

→ Design agent writes implementation plan
→ Design review agent validates against codebase: READY
→ You review design, approve

→ Implement agent writes code following the plan
→ Impl review agent verifies all acceptance criteria addressed
→ Code review agent checks quality: APPROVED
→ Dashboard shows: "Implementation complete, reviews passed"

→ Test agent runs suite, reports results
→ You do manual acceptance testing, approve

→ Ticket auto-advances to done, code merges
```

Every step is tracked in the ticket. Every review is logged. Every conversation is linked. Nothing is invisible.

### Hands-free mode

For low-priority, well-specified work (P3-P4 chores and bugs), the orchestrator can run the full pipeline autonomously, only pausing for human review at the `verify` stage. The `--auto` flag on `tk advance` enables this:

```
tk advance t-5a3f --auto --through verify
```

This advances through stages automatically, using agents for each phase and agent reviewers for gates, stopping only at `verify` for human sign-off. High-priority or high-risk work still gets human review at every stage.

---

## Part 9: What NOT to Do

1. **Don't build a custom agent framework.** Use Claude Code's built-in subagents, Agent SDK, and Agent Teams. The infrastructure exists — you just need the right agent definitions and orchestration logic.

2. **Don't over-engineer the stage system.** 7 stages for features, fewer for simpler types. Type-dependent pipelines handle the ergonomics. Resist adding sub-stages, parallel tracks, or new ticket types just to get different pipelines.

3. **Don't abandon the filesystem-as-database approach.** It's one of your system's greatest strengths. The redesign adds fields to YAML frontmatter — it doesn't add a database.

4. **Don't try to automate away human judgment.** The redesign adds structure around human review points. It doesn't remove them. The goal is to make human review *efficient* (agent does prep work, human makes decisions), not *unnecessary*.

5. **Don't migrate everything at once.** Phase 1 (ticket pipeline in `tk`) can ship independently. Phase 2-4 can follow iteratively. Old-format tickets should still work during migration.

---

## Decisions (Finalized)

1. **Stage naming: Type-dependent pipelines.**
   ```
   feature:  triage → spec → design → implement → test → verify → done
   bug:      triage → implement → test → verify → done
   chore:    triage → implement → done
   epic:     triage → spec → design → done
   ```
   Skip available for edge cases in any direction (e.g., a "bug" that needs design work).

2. **Review actors: Mandatory at implement, advisory elsewhere.**
   Agent reviews (code-review + impl-review) are mandatory gates at the `implement → test` transition. At all other stages, agent reviews are advisory — feedback is always surfaced in `tk advance` output and logged on the ticket, but doesn't block advancement.

3. **Conversation storage: Both session links and inline summaries.**
   Tickets store session IDs in a `conversations` field for full traceability. Additionally, at the end of each conversational phase, the agent writes a focused decision summary (3-5 bullets) into the ticket's `## Notes` section. Different audiences, different purposes: session links answer "what happened?", inline summaries answer "what was decided?"

4. **Repo naming: `forge`.**
   The consolidated workflow repo is named `forge`. `tk` remains the standalone ticket engine. `forge` is the workshop: skills, agents, dashboard, learning pipeline, data.

5. **Backward compatibility: One release of dual support, then hard cut.**
   One release where `tk` reads both `status` and `stage` fields (prefers `stage`, falls back to `status`). A `tk migrate` command rewrites all tickets in-place. The following release drops `status` support entirely.
   Migration mapping: `open→triage`, `in_progress→implement`, `needs_testing→test`, `closed→done`.

6. **Agent model selection: Opus everywhere, configurable down.**
   All agents run on Opus by default. Per-role model override available via configuration for when quota pressure requires it (e.g., switching review agents to Sonnet). Keep it simple until optimization is needed.
