# Competitive Analysis: tkt vs ticket (tk)

**Date:** 2026-03-02
**Audience:** tkt maintainer
**Purpose:** Identify what ticket does better, what tkt does better, and what each project can learn from the other.

---

## Executive Summary

`tkt` and `ticket` (tk) share significant DNA — both are Go-based, git-backed, markdown+YAML ticket trackers with TUI and MCP server support. They diverged on philosophy: **tkt** invested deep in operational intelligence (commit tracking, effort measurement, central storage, mutation audit), while **ticket** invested deep in pipeline enforcement and agent orchestration (7-stage pipelines, structural gates, risk-scaled review, specialized agent workflows). Neither has what the other built. Combining strengths would produce something meaningfully better than either alone.

---

## Where tkt leads

### 1. Commit Journal & Background Service (Major Advantage)

tkt's background daemon that watches git log and auto-links commits to tickets is its most distinctive feature. The journal captures SHA, files changed, lines added/removed, author, branch, and duration — enabling views that no other lightweight tracker provides:

- `lifecycle` shows a ticket's full commit history with effort summary
- `progress` shows what changed today/this week
- `dashboard` includes recent commits alongside ticket state
- `context` gives complete working context with code history

ticket has zero commit awareness. Related commits are invisible to the system.

### 2. Central Store

tkt's `~/.tickets/<project>/` option keeps ticket files out of the working directory. This is a practical advantage: no `.tickets/` cluttering `git status`, no need to `.gitignore` tickets if you don't want them in the repo, and natural cross-project aggregation.

ticket only supports local `.tickets/` storage.

### 3. Mutation Journal

Every MCP write operation records the `source` ("claude", "codex", "human") to `mutation.log`. This creates a complete audit trail of agent-driven changes independent of git history.

ticket tracks review approvals but not general edits.

### 4. Precomputed Views

`epic-view`, `dashboard`, `progress`, `context`, and `lifecycle` are composite views that synthesize data from tickets + journal + dependencies. These answer practical questions ("what's happening?", "what did I do today?", "how much effort went into this?") that ticket's raw listing commands don't.

### 5. Schema Preservation

tkt's `Extra` map round-trips unknown YAML fields without data loss. Users (or other tools) can add custom frontmatter fields and tkt won't silently drop them.

### 6. Contextual TUI Filters

The filter picker (`f` key) offers structured multi-dimensional filtering in the TUI — more powerful than text search alone.

### 7. JSON Envelope Standard

All JSON output wrapped in `{meta: {command, project, generated_at, version}, data: ...}`. Self-documenting, debuggable, versioned.

---

## Where ticket leads

### 1. Pipeline & Stage System (Major Advantage)

This is ticket's biggest differentiator and the most impactful feature tkt lacks.

ticket defines type-dependent stage pipelines:

```
feature: triage → spec → design → implement → test → verify → done
bug:     triage → implement → test → verify → done
chore:   triage → implement → done
epic:    triage → spec → design → done
```

Each transition is guarded by preconditions (gates). The system structurally prevents:
- Implementing a feature before its design is reviewed
- Closing a task before test results are recorded
- Advancing past verify without human sign-off

tkt's 4-status lifecycle (`open → in_progress → needs_testing → closed`) has no structural enforcement. An agent can jump from `open` to `closed` in one edit, skipping all quality checks. Workflow discipline depends entirely on convention and hope.

**Why this matters for AI agents:** Agents are systematically bad at knowing when to stop, when to ask for review, and when to skip steps. A pipeline with gates compensates for this by making the right process the easy process. Without gates, an autonomous agent will routinely skip design, skip review, and ship undertested code.

**Recommendation:** This is the single highest-impact feature tkt could adopt. The implementation requires:
- A `stage` field in frontmatter (alongside `status` for backward compat)
- A `Pipelines` map defining stage sequences per ticket type
- A `Gates()` function defining preconditions per transition
- An `advance` command that checks gates before transitioning
- A `skip` command with mandatory reason for intentional bypasses

### 2. Gate Enforcement with Risk Scaling (Major Advantage)

ticket's gates are scaled by ticket risk level:

| Risk | Behavior |
|------|----------|
| `low` | All gates advisory (never block) |
| `normal` | Standard enforcement |
| `high` | Strict enforcement |
| `critical` | Strict enforcement (future: additional reviewers) |

This means a P4 chore can flow through gates with minimal friction, while a critical security fix gets mandatory human review at every stage. The same pipeline, different rigor.

tkt has no risk concept. All tickets are treated equally regardless of their blast radius.

**Recommendation:** Add `risk` to frontmatter and use it to scale gate strictness. Even without full pipeline adoption, risk-aware gates on the `needs_testing → closed` transition would prevent premature closure of high-risk changes.

### 3. Review System (Major Advantage)

ticket has structured review records:

```yaml
reviewer: "agent:code-review"   # or "human:steve"
verdict: "approved"             # or "rejected"
comment: "LGTM"
stage: "implement"
timestamp: "2026-03-01T10:00:00Z"
```

Gates can require specific reviewer types. A ticket can't advance from `implement → test` until both a code-review and impl-review agent have approved.

tkt's mutation journal tracks *who changed what*, but not *who approved what*. There's no concept of review, approval, or rejection in the data model.

**Recommendation:** Add a review system. At minimum:
- `review` field in frontmatter (pending/approved/rejected)
- `reviews` section in markdown body (append-only log)
- `tkt review <id> --approve/--reject --reviewer <identity>` command
- MCP tool for review verdicts

### 4. Agent Orchestration (Forge)

ticket powers a complete agent orchestration system called Forge, implemented as Claude Code skills:

| Stage | Skill | Behavior |
|-------|-------|----------|
| triage | `/triage` | Human + agent capture idea, create ticket |
| spec | `/spec` | Build testable acceptance criteria |
| design | `/design` | Write design, validate against codebase |
| implement | `/implement` | Build following approved design |
| test | `/test-ticket` | Run tests, record results |
| verify | `/verify` | Walk through AC with human |

Each skill reads ticket state, dispatches to specialized agents (spec-builder, design-reviewer, code-reviewer, impl-reviewer, test-runner), and advances the pipeline on success. The ticket system *drives* the agent workflow.

tkt provides MCP tools and `agent-instructions`, but agents have to self-organize. There's no system telling an agent "this ticket needs design review before you can implement it."

**Recommendation:** Consider defining a skill/prompt layer on top of tkt's MCP tools. Even without full pipeline adoption, structured prompts that read ticket state and dispatch to appropriate actions would significantly improve agent reliability.

### 5. Skip-with-Audit-Trail

ticket's `skip` command records which stages were bypassed and why:

```
tk skip t-123 --to implement --reason "trivial rename, no design needed"
```

The skipped stages are stored on the ticket (`skipped: [spec, design]`), creating an explicit record of intentional process deviations.

tkt has no equivalent — there's nothing to skip because there are no stages.

### 6. Stage Propagation

When all children of an epic reach `done` in ticket, the parent automatically advances to `done`. This recursive propagation reduces manual bookkeeping for project tracking.

tkt tracks parent-child relationships but doesn't auto-advance parents.

**Recommendation:** Add status propagation. When all children of an epic are `closed`, auto-close the parent. This is a small change with meaningful convenience.

### 7. Inbox / Next-Action Derivation

ticket computes a `NextAction` for every ticket based on its stage and review state:
- `ActionHumanReview` — conversational stage with pending review (needs human)
- `ActionAgentWork` — autonomous stage ready for agent (needs agent)
- `ActionBlocked` — has unresolved dependencies (waiting)
- `ActionHumanInput` — needs human decision (triage stage)

This powers the inbox view: "what needs *me* right now?" tkt's `ready` command answers "what *can* be worked on" but not "what *should* be worked on and by whom."

### 8. Conversational vs. Autonomous Stage Designation

ticket marks certain stages as "conversational" (triage, spec, verify) — meaning they expect human back-and-forth — and others as "autonomous" (design, implement, test) — meaning agents can work independently. This distinction drives routing in the inbox: human review items go to humans, agent work items go to agents.

tkt makes no such distinction.

---

## Shared Strengths

Both projects have:
- Markdown + YAML frontmatter storage
- Dependency management with cycle detection
- Symmetric links
- Ready/blocked queries with hierarchy awareness
- MCP server with typed tools
- Bubbletea TUI with board and detail views
- `query` with jq integration
- `stats` and `timeline` analytics
- Human-readable partial ID matching
- Cross-platform Go binary

---

## Architecture Comparison

| Dimension | tkt | ticket (tk) |
|-----------|-----|-------------|
| CLI framework | Custom dispatcher | Cobra (spf13/cobra) |
| MCP SDK | mark3labs/mcp-go | modelcontextprotocol/go-sdk |
| Go version | 1.25.0 | 1.25.6 |
| Total LOC | ~16,700 | ~10,700 |
| Package structure | internal/{ticket,cli,tui,mcp,engine,journal,project} | pkg/ticket + cmd + internal/{tui,mcp} |
| Storage | Local or central | Local only |
| Commit tracking | Background service + journal | None |
| Pipeline | 4-status flat lifecycle | 7-stage with gates |
| Review system | None | Structured records with verdicts |
| Risk scaling | None | low/normal/high/critical |
| Workflow enforcement | Convention-based | Structural (gates block transitions) |
| Agent orchestration | MCP tools + instructions | Forge skills + specialist agents |
| Audit trail | Mutation journal (edits) | Review records (approvals) |
| Schema extensibility | Extra map (preserves unknowns) | Strict struct (drops unknowns) |

---

## Prioritized Recommendations for tkt

### High Impact, High Effort (but highest ROI)
1. **Stage pipeline system** — Add `stage` field, type-dependent pipelines, and gate enforcement. This is the biggest quality improvement available. Without it, agents have no guardrails.
2. **Review system** — Structured review records with reviewer identity and verdict. Gates can then require reviews before advancement.

### High Impact, Moderate Effort
3. **Risk field + scaling** — Add `risk` to frontmatter, use it to scale gate enforcement. Even without full pipelines, risk-aware transitions prevent premature closure of high-impact changes.
4. **Status propagation** — Auto-close parent epics when all children close. Small change, meaningful convenience.

### Medium Impact, Low Effort
5. **Skip-with-audit** — If pipelines are adopted, add `skip` command with mandatory reason and record on ticket.
6. **Inbox / next-action** — Compute what needs attention and who should handle it (human vs. agent). Powers better routing.
7. **Conversational stage markers** — Tag stages that need human interaction vs. autonomous agent work.

### Consider Later
8. **Agent orchestration layer** — Structured prompts/skills that read ticket state and dispatch agents. Depends on pipeline adoption.
9. **Specialist agent types** — Design-reviewer, code-reviewer, spec-builder as distinct agent roles with different prompts.

---

## The 1+1=3 Opportunity

The ideal combination would be:
- tkt's commit journal and central store as the operational data layer
- ticket's pipeline/gates/review system as the workflow enforcement engine
- ticket's forge integration as the agent orchestration layer
- tkt's precomputed views (dashboard, progress, lifecycle) enriched with pipeline stage data
- tkt's mutation journal combined with ticket's review records for complete audit trail
- Shared: dependency management, TUI, MCP server

A tracker with stage-gated workflows AND commit history AND effort tracking AND risk-scaled review enforcement AND cross-project aggregation would be genuinely best-in-class for AI-augmented development.

The projects complement more than they compete. The question is whether to merge, adopt features from each other, or build a shared core library.
