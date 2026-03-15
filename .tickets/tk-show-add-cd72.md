---
id: tk-show-add-cd72
stage: triage
risk: low
deps: []
links: []
created: 2026-03-15T05:46:36Z
type: feature
priority: 3
---
# 'tk show' add support for --metadata

--metadata should only return frontmatter fields and description, no other notes

## Notes

**2026-03-15T22:44:17Z**

## Triage

**Risk:** low — Isolated change to `cmd/show.go`, adds a new non-breaking flag. No auth/PII/schema/API contract impact.

**Priority:** 3 — Convenience feature, not blocking other work.

**Scope:** Single task. Add `--metadata` bool flag to show command to filter output to frontmatter + description only (omit notes, reviews, relationship sections).

**Key decisions:**
- Risk low, priority 3 confirmed (human)
