---
id: tk-ui-pipeline-3c52
stage: backlog
deps: []
links: []
created: 2026-03-15T06:22:43Z
type: bug
priority: 0
---
# 'tk ui' pipeline view is unreachable — no keybinding to switch to it

The pipeline (kanban) view exists as a full implementation in pipeline.go, but there is no keybinding or mechanism to set `a.current = viewPipeline` from the dashboard. The view is dead code from the user's perspective.
