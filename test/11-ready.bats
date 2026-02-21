#!/usr/bin/env bats
# Tests for: tk ready (Spec 7.13)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "ready: shows open tickets with no deps" {
    create_ticket "Ready one" > /dev/null
    run "$TK" ready
    assert_success
    assert_output --partial "Ready one"
}

@test "ready: excludes tickets with unresolved deps" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Blocked")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" ready
    assert_success
    refute_output --partial "Blocked"
    assert_output --partial "Blocker"
}

@test "ready: includes ticket when all deps are closed" {
    local a b
    b=$(create_ticket "Resolved dep")
    a=$(create_ticket "Unblocked")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" edit "$b" --status closed > /dev/null
    run "$TK" ready
    assert_success
    assert_output --partial "Unblocked"
}

@test "ready: shows in_progress tickets" {
    local id
    id=$(create_ticket "In progress ready")
    "$TK" edit "$id" --status in_progress > /dev/null
    run "$TK" ready
    assert_success
    assert_output --partial "In progress ready"
}

@test "ready: excludes closed tickets" {
    local id
    id=$(create_ticket "Closed not ready")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" ready
    assert_success
    refute_output --partial "Closed not ready"
}

@test "ready: excludes needs_testing tickets" {
    local id
    id=$(create_ticket "Testing not ready")
    "$TK" edit "$id" --status needs_testing > /dev/null
    run "$TK" ready
    assert_success
    refute_output --partial "Testing not ready"
}

@test "ready: hierarchy gating excludes children of non-in_progress parents" {
    local epic child
    epic=$(create_ticket "Open epic" -t epic)
    child=$(create_ticket "Gated child" --parent "$epic")
    # Epic is open (not in_progress) — child should be gated
    run "$TK" ready
    assert_success
    refute_output --partial "Gated child"
}

@test "ready: --open bypasses hierarchy gating" {
    local epic child
    epic=$(create_ticket "Open epic" -t epic)
    child=$(create_ticket "Ungated child" --parent "$epic")
    run "$TK" ready --open
    assert_success
    assert_output --partial "Ungated child"
}

@test "ready: children of in_progress parent are ready" {
    local epic child
    epic=$(create_ticket "Active epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    child=$(create_ticket "Active child" --parent "$epic")
    run "$TK" ready
    assert_success
    assert_output --partial "Active child"
}

@test "ready: filter by assignee" {
    create_ticket "Alice ready" -a "alice" > /dev/null
    create_ticket "Bob ready" -a "bob" > /dev/null
    run "$TK" ready -a alice
    assert_success
    assert_output --partial "Alice ready"
    refute_output --partial "Bob ready"
}

@test "ready: filter by tag" {
    create_ticket "Tagged" --tags "urgent" > /dev/null
    create_ticket "Untagged" > /dev/null
    run "$TK" ready -T urgent
    assert_success
    assert_output --partial "Tagged"
    refute_output --partial "Untagged"
}

@test "ready: sorted by priority then ID" {
    create_ticket "P3 ticket" -p 3 > /dev/null
    create_ticket "P1 ticket" -p 1 > /dev/null
    run "$TK" ready
    assert_success
    # P1 should appear before P3
    local p1_line p3_line
    p1_line=$(echo "$output" | grep -n "P1 ticket" | head -1 | cut -d: -f1)
    p3_line=$(echo "$output" | grep -n "P3 ticket" | head -1 | cut -d: -f1)
    [[ "$p1_line" -lt "$p3_line" ]]
}

@test "ready: output format matches spec" {
    create_ticket "Format test" -p 2 > /dev/null
    run "$TK" ready
    assert_success
    # Format: ID  [P<n>][<status>] - <title>
    [[ "$output" =~ \[P[0-4]\]\[(open|in_progress)\]\ -\  ]]
}

@test "ready: unknown flags silently ignored" {
    create_ticket "Permissive" > /dev/null
    run "$TK" ready --bogus
    assert_success
    assert_output --partial "Permissive"
}

@test "ready: multi-level gating requires all ancestors in_progress" {
    local epic child gc
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    child=$(create_ticket "Child" --parent "$epic")
    # child is open, not in_progress
    gc=$(create_ticket "Grandchild" --parent "$child")
    run "$TK" ready
    assert_success
    # Grandchild should NOT be ready because child is not in_progress
    refute_output --partial "Grandchild"
    # Child SHOULD be ready
    assert_output --partial "Child"
}
