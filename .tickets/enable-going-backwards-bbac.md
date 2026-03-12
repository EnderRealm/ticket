---
id: enable-going-backwards-bbac
stage: done
status: open
risk: low
deps: []
links: []
created: 2026-03-11T06:04:44Z
type: feature
priority: 1
skipped: [spec, design, implement, test, verify]
---
# Enable going backwards in stages to enable failure on gates to revert to previous stages

When the user does a verify and there are fundamental issues you may need to go back to spec, design, or implement stages. If during an implementation review it's found the acceptance criteria aren't met you may need to go back to implement stage.

## Design

New `Revert()` function in workflow.go validates target is backward in the pipeline, resets review state, appends audit note. Exposed via `tk revert` CLI command and `ticket_revert` MCP tool. Both require `--to` and `--reason` flags.

## Acceptance Criteria

- `tk revert <id> --to <stage> --reason '...'` moves ticket backward
- `ticket_revert` MCP tool with same behavior
- Reverting forward or to same stage returns error
- Reverting without reason returns error
- Reverting to invalid stage returns error
- Review state reset on revert
- Audit note appended with format "Reverted from X to Y: reason"
