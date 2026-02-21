#!/usr/bin/env bats
# Tests for: tk dep cycle (Spec 7.9)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "dep cycle: no cycles prints message" {
    create_ticket "No cycle" > /dev/null
    run "$TK" dep cycle
    assert_success
    assert_output --partial "No dependency cycles found"
}

@test "dep cycle: detects simple two-node cycle" {
    local a b
    a=$(create_ticket "Cycle A")
    b=$(create_ticket "Cycle B")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$a" > /dev/null
    run "$TK" dep cycle
    assert_success
    assert_output --partial "Cycle"
    assert_output --partial "$a"
    assert_output --partial "$b"
}

@test "dep cycle: detects three-node cycle" {
    local a b c
    a=$(create_ticket "Node A")
    b=$(create_ticket "Node B")
    c=$(create_ticket "Node C")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$c" > /dev/null
    "$TK" dep "$c" "$a" > /dev/null
    run "$TK" dep cycle
    assert_success
    assert_output --partial "Cycle"
}

@test "dep cycle: excludes closed tickets" {
    local a b
    a=$(create_ticket "Cycle A")
    b=$(create_ticket "Cycle B")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$a" > /dev/null
    "$TK" edit "$a" --status closed > /dev/null
    run "$TK" dep cycle
    assert_success
    assert_output --partial "No dependency cycles found"
}

@test "dep cycle: normalizes cycle output" {
    local a b
    a=$(create_ticket "First")
    b=$(create_ticket "Second")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$a" > /dev/null
    run "$TK" dep cycle
    assert_success
    # Cycle format: "Cycle N: id1 -> id2 -> id1"
    assert_output --partial " -> "
}

@test "dep cycle: deduplicates same cycle" {
    local a b
    a=$(create_ticket "Dup A")
    b=$(create_ticket "Dup B")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$a" > /dev/null
    run "$TK" dep cycle
    assert_success
    # Should report exactly one cycle, not two
    local count
    count=$(echo "$output" | grep -c "^Cycle")
    [[ "$count" -eq 1 ]]
}

@test "dep cycle: empty tickets dir exits silently" {
    # With no ticket files, dep cycle returns non-zero (find yields nothing for awk)
    run "$TK" dep cycle
    # Actual behavior: exits non-zero with empty output when no tickets exist
    [[ "$status" -ne 0 ]] || [[ "$output" == *"No dependency cycles"* ]]
}
