---
id: externalize-pipeline-configuration-50a4
stage: triage
status: open
deps: []
links: []
created: 2026-03-06T08:01:08Z
type: epic
priority: 1
assignee: Steve Macbeth
tags: [pipeline, architecture]
---
# Externalize pipeline configuration to JSON and add risk-based variants

Pipeline stage sequences are hardcoded in pkg/ticket/pipeline.go as a Go map. Stage gate checks are hardcoded in gates.go. This makes pipeline changes require code changes and recompilation, and prevents risk-level-based pipeline variants.

**Goals:**

1. **Externalize to JSON** — Move pipeline definitions from Go source to a JSON config file in the tk source repo (e.g. pkg/ticket/pipelines.json). The Go build embeds or code-generates from this file so pipeline structure is compiled into the tk binary. Projects do not customize pipelines at runtime — tk is the single source of truth.

2. **Add review stages** — New discrete stages: design-review and code-review. Currently reviews are implicit gates within generator stages on the forge side. Making them first-class pipeline stages in tk enables the orchestrator to dispatch them independently.

3. **Risk-based pipeline variants** — Each ticket type can have multiple pipeline variants keyed by risk level. Risk is set during spec. Examples:
   - feature/low: triage > spec > design > implement > test > verify > done (skip review stages)
   - feature/normal: triage > spec > design > design-review > implement > code-review > test > verify > done
   - feature/high: same as normal but with human gates on review stages
   - bug/low: triage > implement > done
   - bug/normal: triage > implement > code-review > test > verify > done

4. **Expose via MCP** — Add a ticket_pipelines MCP tool (or extend ticket_workflow) that returns the full pipeline config so forge can read it rather than maintaining a duplicate.

5. **Backwards compatibility** — Existing tickets with current stages continue to work. New stages are additive.

**Affected areas:**
- pkg/ticket/pipeline.go — Replace hardcoded Pipelines map with config-loaded data
- pkg/ticket/gates.go — Gate definitions driven by config
- pkg/ticket/workflow.go — Advance/NextStage reads from loaded config
- pkg/ticket/ticket.go — New stage constants (design-review, code-review)
- cmd/pipeline.go — Display adapts to configured stages
- internal/tui/pipeline.go — TUI kanban adapts to configured stages
- New: JSON config file with pipeline definitions
- New: config loading module (embed or codegen)
- New or updated: MCP tool to expose pipeline config

**Config file shape (strawman):**
{
  "stages": ["triage", "spec", "design", "design-review", "implement", "code-review", "test", "verify", "done"],
  "pipelines": {
    "feature": {
      "low": ["triage", "spec", "design", "implement", "test", "verify", "done"],
      "normal": ["triage", "spec", "design", "design-review", "implement", "code-review", "test", "verify", "done"],
      "high": ["triage", "spec", "design", "design-review", "implement", "code-review", "test", "verify", "done"],
      "critical": ["triage", "spec", "design", "design-review", "implement", "code-review", "test", "verify", "done"]
    },
    "bug": { ... },
    "task": { ... },
    "chore": { ... },
    "epic": { ... }
  },
  "gates": {
    "spec>design": { "require": ["acceptance"] },
    "design>design-review": { "require": ["design"] },
    "design-review>implement": { "require": ["review:design-review"] },
    ...
  }
}
