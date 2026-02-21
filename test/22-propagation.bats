#!/usr/bin/env bats
# Tests for: Status propagation (Spec 6.1)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "propagation: all children closed -> parent closed" {
    local epic c1 c2
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child 1" --parent "$epic")
    c2=$(create_ticket "Child 2" --parent "$epic")
    "$TK" edit "$c1" --status closed > /dev/null
    "$TK" edit "$c2" --status closed > /dev/null
    run read_field "$epic" "status"
    assert_output "closed"
}

@test "propagation: all children needs_testing or closed -> parent needs_testing" {
    local epic c1 c2
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child 1" --parent "$epic")
    c2=$(create_ticket "Child 2" --parent "$epic")
    "$TK" edit "$c1" --status needs_testing > /dev/null
    "$TK" edit "$c2" --status closed > /dev/null
    run read_field "$epic" "status"
    assert_output "needs_testing"
}

@test "propagation: not triggered for open transition" {
    local epic c1
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child" --parent "$epic")
    "$TK" edit "$c1" --status in_progress > /dev/null
    "$TK" edit "$c1" --status open > /dev/null
    # Parent should remain in_progress
    run read_field "$epic" "status"
    assert_output "in_progress"
}

@test "propagation: not triggered for in_progress transition" {
    local epic c1
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child" --parent "$epic")
    "$TK" edit "$c1" --status in_progress > /dev/null
    run read_field "$epic" "status"
    assert_output "in_progress"
}

@test "propagation: recursive up the chain" {
    local root mid leaf
    root=$(create_ticket "Root epic" -t epic)
    "$TK" edit "$root" --status in_progress > /dev/null
    mid=$(create_ticket "Mid epic" -t epic --parent "$root")
    "$TK" edit "$mid" --status in_progress > /dev/null
    leaf=$(create_ticket "Leaf task" --parent "$mid")
    "$TK" edit "$leaf" --status closed > /dev/null
    # Mid should close (only child closed)
    run read_field "$mid" "status"
    assert_output "closed"
    # Root should also close (only child is mid, now closed)
    run read_field "$root" "status"
    assert_output "closed"
}

@test "propagation: partial children prevent parent closure" {
    local epic c1 c2
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child 1" --parent "$epic")
    c2=$(create_ticket "Child 2" --parent "$epic")
    "$TK" edit "$c1" --status closed > /dev/null
    # c2 still open — parent should NOT propagate
    run read_field "$epic" "status"
    assert_output "in_progress"
}

@test "propagation: no parent means no propagation" {
    local id
    id=$(create_ticket "Orphan")
    "$TK" edit "$id" --status closed > /dev/null
    # Should succeed without errors (no parent to propagate to)
    run read_field "$id" "status"
    assert_output "closed"
}

@test "propagation: parent comment stripped for resolution" {
    local epic child
    epic=$(create_ticket "Commented parent" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    child=$(create_ticket "Child" --parent "$epic")
    # Parent field may have comment: "epic-id  # Parent Title"
    "$TK" edit "$child" --status closed > /dev/null
    run read_field "$epic" "status"
    assert_output "closed"
}
