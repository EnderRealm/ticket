---
id: t-3ce6
status: closed
deps: []
links: []
created: 2026-02-22T18:05:14Z
type: feature
priority: 4
assignee: Steve Macbeth
---
# Add 'tk <command> --repo <repo>





Add support to use tk from anywhere against any repo. Should validate that the directory is a valid repo.

## Notes

**2026-02-26T06:52:26Z**

## Brainstorm

**Decision:** (human) --repo points at repo root, walks up to find .tickets/ same as CWD logic.

## Plan

1. Add `--repo` as persistent flag on rootCmd
2. Modify `TicketsDir()` to check --repo first: walk up from given path to find .tickets/
3. Validate that .tickets/ exists when --repo is provided (error if not found)
4. Update help text to document --repo
5. Update CHANGELOG.md

Priority order: --repo flag → TICKETS_DIR env → walk up CWD → fallback .tickets

<!-- checkpoint: planning -->
