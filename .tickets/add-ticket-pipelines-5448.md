---
id: add-ticket-pipelines-5448
stage: done
status: open
deps: [externalize-pipelines-gates-2c14]
links: []
created: 2026-03-06T08:15:31Z
type: task
priority: 1
assignee: Steve Macbeth
parent: externalize-pipeline-configuration-50a4
tags: [pipeline, mcp]
---
# Add ticket_pipelines MCP tool

Expose the full pipeline configuration as a structured MCP tool for forge consumption.\n\n**Scope:**\n- New `ticket_pipelines` tool returns the full JSON config: all stages, all pipeline variants (keyed by type + risk), and gate definitions per transition\n- Machine-readable structured output (distinct from human-readable `ticket_workflow`)\n- Update `ticket_workflow` to generate its text from config data instead of hardcoded string\n- Update `ticket_advance` response to include structured gate info: name, description, type (structural vs agentic), status (pass/fail/unevaluated)\n- `ticket_advance` accepts optional `evidence` map for agentic gate attestation\n- Gate failures return structured objects, not flat error strings

## Test Results

## Test Results\n\nAll tests pass:\n```\nok  github.com/EnderRealm/ticket/internal/mcp  0.289s\nok  github.com/EnderRealm/ticket/internal/tui  (cached)\nok  github.com/EnderRealm/ticket/pkg/ticket    0.520s\n```\n\nBuild clean: `go build ./...` zero errors.

## Review Log

**2026-03-06T08:29:29Z [agent:code-review]**
APPROVED — All tests pass, new MCP tools registered correctly

**2026-03-06T08:29:33Z [agent:impl-review]**
APPROVED — Structured gate results, evidence attestation, pipelines endpoint all clean

**2026-03-06T08:36:20Z [human:steve]**
APPROVED — Stage roles added, workflow shows variants

## Notes

**2026-03-06T08:28:03Z**

<!-- checkpoint: planning -->\n\n## Plan\n1. Add `ticket_pipelines` MCP tool — returns full config as structured JSON\n2. Update `ticket_advance` — accept `evidence` param, return structured gate results via EvaluateGates()\n3. Update advance error responses to return structured gate objects\n4. Tests\n\nNote: ticket_workflow update already done in task 1.

**2026-03-06T08:29:28Z**

<!-- checkpoint: executing -->\n\nImplemented:\n1. New `ticket_pipelines` MCP tool with optional type/risk filters\n2. Updated `ticket_advance` MCP tool: accepts `evidence` map, returns structured `advanceResultJSON` with gate results\n3. Gate failures now return structured objects (GateResult) not flat strings\n4. CHANGELOG updated
