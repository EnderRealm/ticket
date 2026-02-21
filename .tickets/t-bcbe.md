---
id: t-bcbe
status: closed
deps: [t-1d05, t-fce5]
links: []
created: 2026-02-20T23:55:32Z
type: task
priority: 2
assignee: Steve Macbeth
parent: t-571a
---
# Analytics and meta command tests

Write BATS tests for query, stats, timeline, workflow, and help commands. ~40 tests across 5 files.

## Design

Files: test/14-query.bats (12 tests, Spec 7.16/Sec 8), test/15-stats.bats (9 tests, Spec 7.17), test/16-timeline.bats (7 tests, Spec 7.18), test/17-workflow.bats (6 tests, Spec 7.19), test/18-help.bats (6 tests, Spec 7.21/Sec 13).
Key tests: JSONL format, priority-as-string, array typing, snake_case body keys, hyphen frontmatter keys, empty section omission, JSON escaping (backslash/quotes/newlines/tabs), jq filter wrapping, status/type/priority breakdown, age calculation (skip if no mktime), timeline bars, week bucketing, static workflow output, help dispatch for all entry points, unknown command error.

## Acceptance Criteria

All 40 tests pass. Query contract (Sec 8) fully verified. JSON escaping covers all special characters. Stats age tests skip gracefully without gawk.

