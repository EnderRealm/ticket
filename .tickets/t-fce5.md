---
id: t-fce5
status: closed
deps: [t-1d05]
links: []
created: 2026-02-20T23:55:01Z
type: task
priority: 2
assignee: Steve Macbeth
parent: t-571a
---
# Create test helpers

Create shared BATS helper files: common.bash (TK path, setup_fresh_tickets_dir, NO_COLOR, assert_yaml_field, assert_perf) and fixtures.bash (create_ticket, read_field, write_raw_ticket, set_mtime, fixture_dep_chain, fixture_hierarchy, fixture_at_scale).

## Design

Files: test/helpers/common.bash, test/helpers/fixtures.bash
common.bash: PROJECT_ROOT, TK var, setup_fresh_tickets_dir (creates TICKETS_DIR in BATS_TEST_TMPDIR, sets NO_COLOR=1), assert_yaml_field, assert_perf.
fixtures.bash: create_ticket (wraps tk create, returns ID only), read_field (sed YAML parse), write_raw_ticket (cat to file), set_mtime (touch -t), fixture_dep_chain (A->B->C), fixture_hierarchy (epic>children>grandchild), fixture_at_scale (N tickets with varied attrs).

## Acceptance Criteria

Helpers loadable from any .bats file via load 'helpers/common' and load 'helpers/fixtures'. create_ticket returns just the ID. setup_fresh_tickets_dir creates isolated dir per test.

