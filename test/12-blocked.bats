#!/usr/bin/env bats
# Tests for: tk blocked (Spec 7.14)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "blocked: shows tickets with unresolved deps" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Blocked")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" blocked
    assert_success
    assert_output --partial "Blocked"
    assert_output --partial "$b"
}

@test "blocked: excludes tickets with all deps closed" {
    local a b
    b=$(create_ticket "Resolved")
    a=$(create_ticket "Unblocked")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" edit "$b" --status closed > /dev/null
    run "$TK" blocked
    assert_success
    refute_output --partial "Unblocked"
}

@test "blocked: excludes closed tickets" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Closed blocked")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" edit "$a" --status closed > /dev/null
    run "$TK" blocked
    assert_success
    refute_output --partial "Closed blocked"
}

@test "blocked: shows only non-closed blockers in list" {
    local a b c
    b=$(create_ticket "Open blocker")
    c=$(create_ticket "Closed blocker")
    a=$(create_ticket "Multi blocked")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$a" "$c" > /dev/null
    "$TK" edit "$c" --status closed > /dev/null
    run "$TK" blocked
    assert_success
    assert_output --partial "$b"
}

@test "blocked: filter by assignee" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Alice blocked" -a "alice")
    "$TK" dep "$a" "$b" > /dev/null
    create_ticket "Bob blocked" -a "bob" > /dev/null
    run "$TK" blocked -a alice
    assert_success
    assert_output --partial "Alice blocked"
    refute_output --partial "Bob blocked"
}

@test "blocked: filter by tag" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Tagged blocked" --tags "urgent")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" blocked -T urgent
    assert_success
    assert_output --partial "Tagged blocked"
}

@test "blocked: sorted by priority then ID" {
    local b a1 a2
    b=$(create_ticket "Blocker")
    a1=$(create_ticket "P3 blocked" -p 3)
    a2=$(create_ticket "P1 blocked" -p 1)
    "$TK" dep "$a1" "$b" > /dev/null
    "$TK" dep "$a2" "$b" > /dev/null
    run "$TK" blocked
    assert_success
    local p1_line p3_line
    p1_line=$(echo "$output" | grep -n "P1 blocked" | head -1 | cut -d: -f1)
    p3_line=$(echo "$output" | grep -n "P3 blocked" | head -1 | cut -d: -f1)
    [[ "$p1_line" -lt "$p3_line" ]]
}

@test "blocked: output format shows blocker IDs" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Blocked")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" blocked
    assert_success
    assert_output --partial "<-"
    assert_output --partial "$b"
}

@test "blocked: unknown flags silently ignored" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Blocked")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" blocked --bogus
    assert_success
}
