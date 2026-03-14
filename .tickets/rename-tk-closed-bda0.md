---
id: rename-tk-closed-bda0
stage: triage
risk: low
deps: []
links: []
created: 2026-03-12T19:06:20Z
type: bug
priority: 2
---
# Rename 'tk closed' to 'tk done'

The CLI command `tk closed` should be renamed to `tk done` to align with pipeline stage terminology. Tickets end in the \"done\" stage, not \"closed\" status — the command name should reflect this. Rename the command, update help text, and update any internal references.
