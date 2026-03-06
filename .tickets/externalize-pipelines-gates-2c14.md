---
id: externalize-pipelines-gates-2c14
stage: done
status: open
deps: []
links: []
created: 2026-03-06T08:15:23Z
type: task
priority: 1
assignee: Steve Macbeth
parent: externalize-pipeline-configuration-50a4
tags: [pipeline, architecture]
---
# Externalize pipelines and gates to embedded JSON config

Foundation task: move pipeline definitions and gate declarations from hardcoded Go maps/functions to an embedded JSON config file.\n\n**Scope:**\n- Create `pkg/ticket/pipelines.json` with stage list, pipeline definitions per type, and gate declarations\n- Add new stage constants: `design-review`, `code-review`\n- Load config via `//go:embed` at package init\n- Replace `Pipelines` map in `pipeline.go` with config-loaded data\n- Convert gates to hybrid model (Option C): structural checks remain server-side Go functions, agentic checks are declared in config and returned as requirements\n- `PipelineFor()` becomes risk-aware: accepts risk level, returns the variant pipeline\n- Update `ValidateStage()` to accept config-defined stages dynamically\n- Update `cmd/pipeline.go` to read stages from config instead of hardcoded list\n- Update `internal/tui/pipeline.go` `allStages` to read from config\n- Update `ticket_workflow` MCP tool to generate output from config\n- Update `tk help` / root command help text\n- Backwards compatibility: existing tickets with current stages continue to work\n\n**Gate model (Option C):**\n- Structural gates: named check functions (description_exists, acceptance_exists, design_exists, review_approved, test_results_recorded) stay as Go implementations\n- Agentic gates: declared in JSON with description string, returned to caller as requirements to attest to\n- `ticket_advance` accepts optional `evidence` parameter for agentic gate attestation

## Test Results

## Test Results\n\nAll tests pass:\n```\nok  github.com/EnderRealm/ticket/internal/mcp  0.812s\nok  github.com/EnderRealm/ticket/internal/tui  0.575s\nok  github.com/EnderRealm/ticket/pkg/ticket    0.372s\n```\n\nKey test coverage:\n- pipeline_test.go: PipelineFor, HasStage, NextStage, PrevStage, StageIndex, IsFinalStage all pass with config-loaded data\n- gates_test.go: all structural gate checks pass, low-risk gates now enforced (not advisory), chore advisory gate works\n- mcp_test.go: MCP server tests pass (workflow tool, advance, etc.)\n- Build clean: `go build ./...` with zero warnings

## Review Log

**2026-03-06T08:23:31Z [agent:code-review]**
APPROVED — All tests pass, build clean, backward-compatible API preserved

**2026-03-06T08:23:32Z [agent:impl-review]**
APPROVED — Config-driven architecture, hybrid gate model, risk-aware pipeline selection

**2026-03-06T08:27:39Z [human:steve]**
APPROVED — Approved for commit and push

## Notes

**2026-03-06T08:16:40Z**

<!-- checkpoint: planning -->\n\n## Plan\n1. Create pkg/ticket/pipelines.json\n2. Create pkg/ticket/config.go (embed + parse)\n3. Rewrite pipeline.go to read from config\n4. Rewrite gates.go for hybrid model (structural + agentic)\n5. Update ticket.go stage validation to be config-driven\n6. Update callers: workflow.go, cmd/pipeline.go, tui/pipeline.go, mcp/mcp.go\n7. Run tests, fix breakage

**2026-03-06T08:23:23Z**

<!-- checkpoint: executing -->\n\nAll code changes complete:\n- pkg/ticket/pipelines.json: full pipeline config with stages, type variants, risk variants, and gate declarations\n- pkg/ticket/config.go: embed + parse + accessor functions\n- pkg/ticket/pipeline.go: reads from config, PipelineFor() accepts risk, backward-compatible Pipelines map\n- pkg/ticket/gates.go: hybrid model with structural + agentic gates, EvaluateGates() for structured results\n- pkg/ticket/ticket.go: new StageDesignReview/StageCodeReview constants, config-driven ValidateStage\n- pkg/ticket/workflow.go: risk-aware Advance()\n- cmd/pipeline.go, cmd/workflow.go, cmd/root.go: config-driven stages and help text\n- internal/tui/pipeline.go, internal/tui/form.go: config-driven stages and colors\n- internal/mcp/mcp.go: ticket_workflow generates from config\n- CHANGELOG.md updated
