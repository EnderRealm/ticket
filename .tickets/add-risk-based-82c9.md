---
id: add-risk-based-82c9
stage: done
status: open
deps: [externalize-pipelines-gates-2c14]
links: []
created: 2026-03-06T08:15:28Z
type: task
priority: 1
assignee: Steve Macbeth
parent: externalize-pipeline-configuration-50a4
tags: [pipeline, architecture]
skipped: [implement, test, verify]
---
# Add risk-based pipeline variants

Build on the JSON config foundation to support risk-based pipeline variants.\n\n**Scope:**\n- JSON config defines per-type pipeline variants keyed by risk level (low, normal, high, critical)\n- `PipelineFor(type, risk)` selects the correct variant, falling back to `normal` if no variant for that risk\n- Remove `applyRiskScaling()` — risk-based gate scaling is replaced by variant pipelines\n- Risk level is passed through to agents/skills via MCP responses so they can adjust behavior\n- Different risk levels can have different stage sequences (e.g., low skips review stages)\n- High/critical can have human-required gates on review stages\n\n**Example variants:**\n- feature/low: triage > spec > design > implement > test > verify > done\n- feature/normal: triage > spec > design > design-review > implement > code-review > test > verify > done\n- bug/low: triage > implement > done\n- bug/normal: triage > implement > code-review > test > verify > done
