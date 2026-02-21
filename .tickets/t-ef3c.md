---
id: t-ef3c
status: closed
deps: [t-1d05, t-fce5]
links: []
created: 2026-02-20T23:55:09Z
type: task
priority: 2
assignee: Steve Macbeth
parent: t-571a
---
# CRUD command tests

Write BATS tests for create, edit, show, delete, add-note, and dep/undep commands. ~81 tests across 6 files.

## Design

Files: test/01-create.bats (23 tests, Spec 7.1), test/02-edit.bats (22 tests, Spec 7.2), test/03-show.bats (13 tests, Spec 7.3), test/04-delete.bats (6 tests, Spec 7.4), test/05-add-note.bats (7 tests, Spec 7.5), test/06-dep.bats (10 tests, Spec 7.6/7.7).
Key tests: all flags, required/optional fields, invalid input rejection, strict unknown flag handling, output format verification, status propagation triggers, computed sections in show, stdin pipe for add-note.

## Acceptance Criteria

All 81 tests pass. Each file independently runnable. Covers every flag and error case in Spec sections 7.1-7.7.

