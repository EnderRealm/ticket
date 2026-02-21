---
id: t-c9b5
status: closed
deps: [t-1d05, t-fce5]
links: []
created: 2026-02-20T23:55:40Z
type: task
priority: 2
assignee: Steve Macbeth
parent: t-571a
---
# Cross-cutting concern tests

Write BATS tests for partial ID resolution, env vars, file format, status propagation, hierarchy gating, error handling, and output format. ~61 tests across 7 files.

## Design

Files: test/19-partial-id.bats (8 tests, Spec 5.3), test/20-env-vars.bats (5 tests, Spec 9), test/21-file-format.bats (11 tests, Spec 5.4-5.7), test/22-propagation.bats (8 tests, Spec 6.1), test/23-hierarchy-gating.bats (5 tests, Spec 6.2), test/24-error-handling.bats (15 tests, Spec 12), test/25-output-format.bats (9 tests, Spec 11).
Key tests: exact/substring/ambiguous/zero ID matching, TICKETS_DIR override, NO_COLOR suppression, YAML frontmatter structure, required fields, recursive multi-level propagation, propagation NOT on open/in_progress, multi-level gating, strict vs permissive flag families, exit codes per error category, exact output format regex matching.

## Acceptance Criteria

All 61 tests pass. Partial ID resolution fully exercised. Both env vars tested. All error categories from Spec 12 covered. Output formats match Spec 11 exactly.

