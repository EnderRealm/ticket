# Competitive Analysis: ticket (tk) vs tkt

**Date:** 2026-03-02
**Audience:** ticket (tk) maintainer
**Purpose:** Identify what tkt does better, what ticket does better, and what each project can learn from the other.

---

## Executive Summary

`ticket` and `tkt` share DNA — both are Go-based, git-backed, markdown+YAML ticket trackers with TUI and MCP server support. They diverged on philosophy: **ticket** invested deep in pipeline enforcement and agent orchestration, while **tkt** invested deep in operational intelligence (commit tracking, effort measurement, central storage). Neither project has what the other built, and combining the strengths would produce something significantly better than either alone.

---

## Where ticket leads

### 1. Pipeline & Stage System (Major Advantage)

ticket's 7-stage pipeline with type-dependent paths is the single biggest differentiator:

```
feature: triage → spec → design → implement → test → verify → done
bug:     triage → implement → test → verify → done
chore:   triage → implement → done
epic:    triage → spec → design → done
```

tkt has a flat 4-status lifecycle (`open → in_progress → needs_testing → closed`). This means tkt has no structural way to enforce that a feature gets a design review before implementation, or that acceptance criteria exist before coding begins. Workflow discipline in tkt relies entirely on conventions and human memory.

**What this means:** ticket can prevent entire categories of quality failures that tkt cannot. An agent working in ticket literally cannot skip design review on a high-risk feature. An agent working in tkt can, and frequently will.

### 2. Gate Enforcement with Risk Scaling

ticket's gate system checks preconditions at every stage transition, scaled by risk level:

- `low` risk: all gates advisory
- `normal`: standard enforcement
- `high`/`critical`: strict enforcement

Gates check for concrete artifacts: "Does the ticket have acceptance criteria?", "Has a design reviewer approved?", "Are test results recorded?"

tkt has no gate concept at all. Status transitions are unconstrained.

### 3. Review System with Audit Trail

ticket tracks reviews as structured records with reviewer identity, verdict, comment, timestamp, and stage. Gates can require specific reviewer types (code-review, impl-review, design-review).

tkt has no review system. The mutation journal tracks *who made changes*, but not *who approved them*.

### 4. Forge Integration (Agent Orchestration)

ticket powers a complete agent orchestration layer via Claude Code skills (`/triage`, `/spec`, `/design`, `/implement`, `/test-ticket`, `/verify`). Each skill maps to a pipeline stage, reads ticket state, and dispatches to specialized agents (spec-builder, design-reviewer, code-reviewer, etc.).

tkt has MCP tools and `agent-instructions` for setup, but no equivalent orchestration layer. Agents using tkt need to figure out their own workflow.

### 5. Skip-with-Audit-Trail

When ticket skips stages (`tk skip --to implement --reason "..."`) it records the skipped stages on the ticket. This creates an audit trail of what was intentionally bypassed and why.

tkt has no concept of skipping — there's nothing to skip because there are no stages.

### 6. Stage Propagation

When all children of an epic reach `done`, the parent automatically advances. When all children reach `test`, the parent advances to `test`. This recursive propagation reduces manual bookkeeping.

tkt tracks parent-child relationships but doesn't auto-advance parents based on children's state.

---

## Where tkt leads

### 1. Central Store (Major Advantage)

tkt supports storing tickets in `~/.tickets/<project>/` instead of (or alongside) the project's `.tickets/` directory. This:

- Keeps working directories clean (no `.tickets/` in every repo)
- Enables cross-project ticket aggregation without symlinks
- Allows ticket management when the project repo isn't checked out
- Supports migration between local and central stores

ticket only supports local `.tickets/` storage (with env var override). This means tickets are always mixed in with the codebase, and cross-project views require walking multiple repos.

**Recommendation:** Add central store support to ticket. The `--repo` flag already walks up to find `.tickets/`, so the infrastructure for path resolution exists. Central store with git-backed sync would be a natural extension.

### 2. Commit Journal & Auto-Linking (Major Advantage)

tkt's background service daemon watches git log and automatically:

- Links commits to tickets via bracket refs (`[ticket-id]`)
- Auto-closes tickets when commits contain `Closes: [ticket-id]`
- Records commit metadata (SHA, files changed, lines added/removed, branch, duration)
- Stores everything in an append-only journal (`~/.tkt/state/<project>/journal.log`)

ticket has zero commit awareness. You can reference tickets in commit messages, but there's no system that reads those references back. The `lifecycle` command in tkt can show a ticket's full commit history with effort summaries — ticket can't do anything like this.

**Recommendation:** This is the single most valuable feature to adopt. A commit journal connecting tickets to code changes would:
- Enable `tk show` to display related commits
- Enable effort tracking (how long did this ticket take?)
- Enable `tk progress` (what got done today/this week?)
- Enable auto-close from commit messages
- Make the audit trail complete: not just "what was reviewed" but "what code was written"

### 3. Mutation Journal (Audit Trail for Writes)

tkt's MCP write tools require a `source` field ("claude", "codex", "human") and append every mutation to `~/.tkt/state/<project>/mutation.log`. This creates a complete audit trail of who changed what, when, and through which interface.

ticket's review records track approvals, but don't track *edits*. If an agent modifies a ticket's description, there's no record of that change (beyond git history of the markdown file).

**Recommendation:** Add mutation logging, at minimum for MCP write operations. The `source` field pattern is clean and low-cost.

### 4. Precomputed Views

tkt has several composite views that ticket lacks:

- **`epic-view`** — Hierarchical view of an epic with all children, cross-dependencies, and linked commits
- **`dashboard`** — Project summary: in-progress, blocked, ready, recent commits
- **`progress`** — What changed today/this week (closed tickets + commits + status transitions)
- **`context`** — Full working context for a ticket (parent, deps, children, commits)
- **`lifecycle`** — Timeline from creation to close with effort summary

ticket has `inbox`, `next`, `pipeline`, and `stats`, but lacks the commit-enriched views and the "what happened recently" views.

**Recommendation:** `dashboard` and `progress` are the highest-value additions. They answer "what's the state of the project?" and "what did I accomplish?" — questions that every developer asks daily. The others become possible once commit tracking is added.

### 5. TUI Epic View

tkt's TUI has a dedicated epic view that shows parent-child hierarchy as a navigable tree. ticket's TUI has pipeline (kanban) and dashboard (inbox) views, but no hierarchy view.

**Recommendation:** Add an epic/hierarchy view to the TUI. The data model already supports it — `Parent` field and `store.List()` provide everything needed.

### 6. Schema Preservation (Extra Fields)

tkt preserves unknown YAML frontmatter fields via an `Extra` map. If someone adds `custom_field: value` to a ticket's YAML, tkt round-trips it without data loss.

ticket's YAML parsing uses strict struct tags. Unknown fields are silently dropped on re-serialize.

**Recommendation:** Add an `Extra map[string]any` field to `Ticket` and handle it in `format.go`. This is a small change with outsized value for interoperability and extensibility.

### 7. Contextual TUI Filters

tkt's TUI has a contextual filter picker (`f` key) that offers "by parent", "by assignee", "by status" as interactive filter options. ticket's TUI has search (`/`) but lacks multi-dimensional filtering.

### 8. JSON Envelope Standard

tkt wraps all JSON output in a standard envelope with metadata (command, project, generated_at, version). ticket outputs raw JSON/JSONL without metadata. The envelope makes responses self-documenting and easier to debug.

---

## Shared Strengths

Both projects have:
- Markdown + YAML frontmatter storage
- Full dependency management with cycle detection
- Symmetric links between tickets
- Ready/blocked queries with hierarchy awareness
- MCP server with typed tools
- Bubbletea TUI with kanban and detail views
- `query` command with jq integration
- `stats` and `timeline` analytics
- File watcher for TUI auto-reload (ticket) / service mode (tkt)
- Human-readable partial ID matching
- Cross-platform Go binary distribution

---

## Shared Weaknesses

Neither project has:
- Multi-user / team support
- CI/CD integration
- PR automation
- Real-time notifications (Slack, email)
- Parallel agent execution
- Web UI
- Rich analytics (cycle time, throughput trends, bottleneck detection)

---

## Architecture Comparison

| Dimension | ticket (tk) | tkt |
|-----------|-------------|-----|
| CLI framework | Cobra (spf13/cobra) | Custom dispatcher |
| MCP SDK | modelcontextprotocol/go-sdk | mark3labs/mcp-go |
| Go version | 1.25.6 | 1.25.0 |
| Total LOC | ~10,700 | ~16,700 |
| Package structure | pkg/ticket + cmd + internal/{tui,mcp} | internal/{ticket,cli,tui,mcp,engine,journal,project} |
| Storage | Local .tickets/ only | Local or central (~/.tickets/<project>/) |
| Commit tracking | None | Background service + journal |
| Pipeline | 7-stage with gates | 4-status flat lifecycle |
| Review system | Structured records with verdicts | None |
| Risk scaling | low/normal/high/critical | None |
| Workflow enforcement | Structural (gates block transitions) | Convention-based (docs only) |
| Agent orchestration | Forge skills + specialist agents | MCP tools + instructions |

---

## Prioritized Recommendations for ticket

### High Impact, Moderate Effort
1. **Commit journal** — Background service or git hook that tracks commit-to-ticket linkage. Enables effort tracking, progress views, and auto-close. This is the biggest gap.
2. **Central store** — Add `~/.tickets/<project>/` as storage option alongside local `.tickets/`. Enables cross-project aggregation.

### High Impact, Low Effort
3. **Schema preservation** — Add `Extra map[string]any` to Ticket struct. Prevents data loss with custom fields.
4. **Mutation logging** — Append MCP write operations to an audit log with source attribution.
5. **Dashboard command** — Composite view: in-progress, blocked, ready, recent activity.

### Medium Impact, Low Effort
6. **Progress command** — "What happened today/this week" with closed tickets and status changes.
7. **Context command** — Full working context for a ticket (parent, deps, children).
8. **JSON envelope** — Wrap `--json` output in standard metadata envelope.

### Consider Later
9. **Epic view in TUI** — Tree hierarchy for parent-child navigation.
10. **Contextual TUI filters** — Multi-dimensional filter picker.
11. **Effort tracking** — Time-in-stage, lines changed, commit count per ticket.

---

## The 1+1=3 Opportunity

The ideal merge would be:
- ticket's pipeline/gates/review system as the workflow engine
- tkt's commit journal and central store as the operational layer
- ticket's forge integration as the agent orchestration layer
- tkt's precomputed views (dashboard, progress, lifecycle) enriched with pipeline data
- Shared: dependency management, TUI, MCP server

A ticket with pipeline gates AND commit history AND effort tracking AND risk-scaled review enforcement would be genuinely best-in-class for solo/small-team AI-augmented development.
