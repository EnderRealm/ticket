---
id: add-ticket-pipelines-5448
stage: triage
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
