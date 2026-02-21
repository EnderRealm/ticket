#!/usr/bin/env bats
# Tests for: tk edit (Spec 7.2)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

# --- Basic editing ---

@test "edit: change status" {
    local id
    id=$(create_ticket "Edit status")
    run "$TK" edit "$id" --status in_progress
    assert_success
    assert_output --partial "Updated"
    run read_field "$id" "status"
    assert_output "in_progress"
}

@test "edit: change priority" {
    local id
    id=$(create_ticket "Edit priority")
    run "$TK" edit "$id" -p 0
    assert_success
    run read_field "$id" "priority"
    assert_output "0"
}

@test "edit: change type" {
    local id
    id=$(create_ticket "Edit type")
    run "$TK" edit "$id" -t feature
    assert_success
    run read_field "$id" "type"
    assert_output "feature"
}

@test "edit: change assignee" {
    local id
    id=$(create_ticket "Edit assignee")
    run "$TK" edit "$id" -a "bob"
    assert_success
    run read_field "$id" "assignee"
    assert_output "bob"
}

@test "edit: change title" {
    local id
    id=$(create_ticket "Old title")
    run "$TK" edit "$id" --title "New title"
    assert_success
    run "$TK" show "$id"
    assert_output --partial "# New title"
}

@test "edit: change description" {
    local id
    id=$(create_ticket "Desc edit" -d "Old desc")
    run "$TK" edit "$id" -d "New desc"
    assert_success
    run "$TK" show "$id"
    assert_output --partial "New desc"
}

@test "edit: upsert design section" {
    local id
    id=$(create_ticket "Design edit")
    run "$TK" edit "$id" --design "New design"
    assert_success
    run "$TK" show "$id"
    assert_output --partial "## Design"
    assert_output --partial "New design"
}

@test "edit: upsert acceptance criteria section" {
    local id
    id=$(create_ticket "AC edit")
    run "$TK" edit "$id" --acceptance "New criteria"
    assert_success
    run "$TK" show "$id"
    assert_output --partial "## Acceptance Criteria"
    assert_output --partial "New criteria"
}

@test "edit: set parent" {
    local epic child
    epic=$(create_ticket "Parent" -t epic)
    child=$(create_ticket "Child")
    run "$TK" edit "$child" --parent "$epic"
    assert_success
    run read_field "$child" "parent"
    assert_output --partial "$epic"
}

@test "edit: set tags" {
    local id
    id=$(create_ticket "Tag edit")
    run "$TK" edit "$id" --tags "api,urgent"
    assert_success
    run read_field "$id" "tags"
    assert_output --partial "api"
    assert_output --partial "urgent"
}

@test "edit: set external-ref" {
    local id
    id=$(create_ticket "ExtRef edit")
    run "$TK" edit "$id" --external-ref "jira-456"
    assert_success
    run read_field "$id" "external-ref"
    assert_output "jira-456"
}

# --- Validation ---

@test "edit: invalid status rejected" {
    local id
    id=$(create_ticket "Bad status")
    run "$TK" edit "$id" --status invalid
    assert_failure
}

@test "edit: invalid priority rejected" {
    local id
    id=$(create_ticket "Bad priority")
    run "$TK" edit "$id" -p 5
    assert_failure
}

@test "edit: no options fails" {
    local id
    id=$(create_ticket "No opts")
    run "$TK" edit "$id"
    assert_failure
}

@test "edit: unknown flag causes failure" {
    local id
    id=$(create_ticket "Unknown flag")
    run "$TK" edit "$id" --bogus "value"
    assert_failure
}

@test "edit: nonexistent ticket fails" {
    run "$TK" edit "nonexistent-0000" --status open
    assert_failure
}

# --- Preservation ---

@test "edit: unmodified fields preserved" {
    local id
    id=$(create_ticket "Preserve" -t feature -p 1 -a "alice")
    "$TK" edit "$id" --status in_progress > /dev/null
    # Other fields should be untouched
    run read_field "$id" "type"
    assert_output "feature"
    run read_field "$id" "priority"
    assert_output "1"
    run read_field "$id" "assignee"
    assert_output "alice"
}

# --- Status transitions ---

@test "edit: all valid status values accepted" {
    local id s
    id=$(create_ticket "Status transitions")
    for s in open in_progress needs_testing closed; do
        run "$TK" edit "$id" --status "$s"
        assert_success
        local actual
        actual=$(read_field "$id" "status")
        [[ "$actual" == "$s" ]]
    done
}

# --- Status propagation trigger ---

@test "edit: closing last child propagates to parent" {
    local epic c1
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Only child" --parent "$epic")
    "$TK" edit "$c1" --status in_progress > /dev/null
    run "$TK" edit "$c1" --status closed
    assert_success
    run read_field "$epic" "status"
    assert_output "closed"
}

@test "edit: needs_testing on all children propagates to parent" {
    local epic c1 c2
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child 1" --parent "$epic")
    c2=$(create_ticket "Child 2" --parent "$epic")
    "$TK" edit "$c1" --status needs_testing > /dev/null
    run "$TK" edit "$c2" --status needs_testing
    assert_success
    run read_field "$epic" "status"
    assert_output "needs_testing"
}

@test "edit: mixed needs_testing and closed propagates needs_testing" {
    local epic c1 c2
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child 1" --parent "$epic")
    c2=$(create_ticket "Child 2" --parent "$epic")
    "$TK" edit "$c1" --status closed > /dev/null
    run "$TK" edit "$c2" --status needs_testing
    assert_success
    run read_field "$epic" "status"
    assert_output "needs_testing"
}
