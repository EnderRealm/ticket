#!/usr/bin/env bats
# Tests for: tk create (Spec 7.1)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

# --- Basic creation ---

@test "create: minimal ticket with just title" {
    run "$TK" create "My ticket"
    assert_success
    assert_output --partial "My ticket"
}

@test "create: returns show output on success" {
    run "$TK" create "Show output ticket"
    assert_success
    # Should contain frontmatter
    assert_output --partial "id:"
    assert_output --partial "status: open"
    assert_output --partial "# Show output ticket"
}

@test "create: default status is open" {
    local id
    id=$(create_ticket "Status test")
    run read_field "$id" "status"
    assert_output "open"
}

@test "create: default priority is 2" {
    local id
    id=$(create_ticket "Priority test")
    run read_field "$id" "priority"
    assert_output "2"
}

@test "create: default type is task" {
    local id
    id=$(create_ticket "Type test")
    run read_field "$id" "type"
    assert_output "task"
}

@test "create: ID matches prefix-hash format" {
    local id
    id=$(create_ticket "ID format test")
    [[ "$id" =~ ^[a-z]+-[0-9a-f]{4}$ ]]
}

# --- Flags ---

@test "create: -d sets description" {
    run "$TK" create "Desc ticket" -d "My description"
    assert_success
    assert_output --partial "My description"
}

@test "create: -t sets type" {
    local id
    id=$(create_ticket "Feature" -t feature)
    run read_field "$id" "type"
    assert_output "feature"
}

@test "create: -p sets priority" {
    local id
    id=$(create_ticket "Urgent" -p 0)
    run read_field "$id" "priority"
    assert_output "0"
}

@test "create: -a sets assignee" {
    local id
    id=$(create_ticket "Assigned" -a "alice")
    run read_field "$id" "assignee"
    assert_output "alice"
}

@test "create: --parent sets parent field" {
    local parent_id child_id
    parent_id=$(create_ticket "Parent" -t epic)
    child_id=$(create_ticket "Child" --parent "$parent_id")
    run read_field "$child_id" "parent"
    assert_output --partial "$parent_id"
}

@test "create: --tags stores flow array" {
    local id
    id=$(create_ticket "Tagged" --tags "ui,backend")
    run read_field "$id" "tags"
    assert_output --partial "ui"
    assert_output --partial "backend"
}

@test "create: --external-ref sets external reference" {
    local id
    id=$(create_ticket "ExtRef" --external-ref "gh-123")
    run read_field "$id" "external-ref"
    assert_output "gh-123"
}

@test "create: --design creates Design section" {
    run "$TK" create "Designed" --design "Architecture notes"
    assert_success
    assert_output --partial "## Design"
    assert_output --partial "Architecture notes"
}

@test "create: --acceptance creates Acceptance Criteria section" {
    run "$TK" create "AC ticket" --acceptance "Must pass tests"
    assert_success
    assert_output --partial "## Acceptance Criteria"
    assert_output --partial "Must pass tests"
}

# --- Required fields ---

@test "create: all required fields present" {
    local id
    id=$(create_ticket "Required fields")
    local file="${TICKETS_DIR}/${id}.md"
    assert_yaml_field "$file" "id" "$id"
    assert_yaml_field "$file" "status" "open"
    run read_field "$id" "deps"
    assert_output "[]"
    run read_field "$id" "links"
    assert_output "[]"
    run read_field "$id" "created"
    # ISO 8601 format
    [[ "$output" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T ]]
}

# --- Validation ---

@test "create: invalid priority rejects creation" {
    run "$TK" create "Bad priority" -p 5
    assert_failure
}

@test "create: negative priority rejects creation" {
    run "$TK" create "Neg priority" -p -1
    assert_failure
}

@test "create: empty title fails" {
    run "$TK" create "" </dev/null
    assert_failure
}

# --- Strict flag handling ---

@test "create: unknown flag causes failure" {
    run "$TK" create "Unknown flag" --bogus "value"
    assert_failure
}

# --- Priority edge cases ---

@test "create: priority 0 (highest) accepted" {
    local id
    id=$(create_ticket "P0" -p 0)
    run read_field "$id" "priority"
    assert_output "0"
}

@test "create: priority 4 (lowest) accepted" {
    local id
    id=$(create_ticket "P4" -p 4)
    run read_field "$id" "priority"
    assert_output "4"
}
