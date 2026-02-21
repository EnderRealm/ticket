#!/usr/bin/env bats
# Smoke test: verify test infrastructure loads and basic operations work

setup() {
    load 'helpers/common'
    setup_fresh_tickets_dir
}

@test "tk binary is executable" {
    run "$TK" help
    assert_success
    assert_output --partial "ticket management CLI"
}

@test "TICKETS_DIR is isolated per test" {
    # setup_fresh_tickets_dir creates the dir — verify it exists and is temporary
    [[ -d "$TICKETS_DIR" ]]
    [[ "$TICKETS_DIR" != *".tickets"* ]]
}

@test "create_ticket returns just the ID" {
    load 'helpers/fixtures'
    local id
    id=$(create_ticket "Smoke test ticket")
    # ID should match prefix-hash pattern (no whitespace, no extra output)
    [[ "$id" =~ ^[a-z]+-[0-9a-f]{4}$ ]]
}

@test "read_field extracts frontmatter values" {
    load 'helpers/fixtures'
    local id
    id=$(create_ticket "Field test" -t feature -p 1)
    run read_field "$id" "type"
    assert_output "feature"
    run read_field "$id" "priority"
    assert_output "1"
}

@test "write_raw_ticket creates a file directly" {
    load 'helpers/fixtures'
    write_raw_ticket "test-0001" "id: test-0001
status: open
deps: []
links: []
created: 2026-01-15T10:00:00Z
type: task
priority: 2" "# Raw ticket

Description here."
    assert_file_exist "${TICKETS_DIR}/test-0001.md"
    run "$TK" show test-0001
    assert_success
    assert_output --partial "Raw ticket"
}

@test "assert_yaml_field validates correctly" {
    load 'helpers/fixtures'
    local id
    id=$(create_ticket "Yaml check" -p 0)
    assert_yaml_field "${TICKETS_DIR}/${id}.md" "priority" "0"
}

@test "fixture_dep_chain creates linked tickets" {
    load 'helpers/fixtures'
    local a b c
    read -r a b c <<< "$(fixture_dep_chain)"
    # a depends on b
    run read_field "$a" "deps"
    assert_output --partial "$b"
    # b depends on c
    run read_field "$b" "deps"
    assert_output --partial "$c"
}

@test "fixture_hierarchy creates parent-child structure" {
    load 'helpers/fixtures'
    local epic c1 c2 gc
    read -r epic c1 c2 gc <<< "$(fixture_hierarchy)"
    # children reference parent
    run read_field "$c1" "parent"
    assert_output --partial "$epic"
    # grandchild references child1
    run read_field "$gc" "parent"
    assert_output --partial "$c1"
}
