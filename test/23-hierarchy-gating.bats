#!/usr/bin/env bats
# Tests for: Hierarchy gating in ready (Spec 6.2 rule 3)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "gating: child of open parent not ready" {
    local epic child
    epic=$(create_ticket "Open epic" -t epic)
    child=$(create_ticket "Gated child" --parent "$epic")
    run "$TK" ready
    assert_success
    refute_output --partial "Gated child"
}

@test "gating: child of in_progress parent is ready" {
    local epic child
    epic=$(create_ticket "Active epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    child=$(create_ticket "Ready child" --parent "$epic")
    run "$TK" ready
    assert_success
    assert_output --partial "Ready child"
}

@test "gating: --open flag bypasses gating" {
    local epic child
    epic=$(create_ticket "Open epic" -t epic)
    child=$(create_ticket "Bypassed child" --parent "$epic")
    run "$TK" ready --open
    assert_success
    assert_output --partial "Bypassed child"
}

@test "gating: multi-level requires all ancestors in_progress" {
    local root mid leaf
    root=$(create_ticket "Root" -t epic)
    "$TK" edit "$root" --status in_progress > /dev/null
    mid=$(create_ticket "Mid" -t epic --parent "$root")
    "$TK" edit "$mid" --status in_progress > /dev/null
    leaf=$(create_ticket "Leaf" --parent "$mid")
    run "$TK" ready
    assert_success
    assert_output --partial "Leaf"
}

@test "gating: broken chain blocks deep children" {
    local root mid leaf
    root=$(create_ticket "Root" -t epic)
    "$TK" edit "$root" --status in_progress > /dev/null
    mid=$(create_ticket "Mid" -t epic --parent "$root")
    # mid is open, not in_progress — breaks the chain
    leaf=$(create_ticket "Blocked leaf" --parent "$mid")
    run "$TK" ready
    assert_success
    refute_output --partial "Blocked leaf"
}
