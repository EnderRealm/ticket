#!/usr/bin/env bats
# Tests for: tk dep and tk undep (Spec 7.6, 7.7)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

# --- dep ---

@test "dep: adds dependency" {
    local a b
    a=$(create_ticket "Dependent")
    b=$(create_ticket "Dependency")
    run "$TK" dep "$a" "$b"
    assert_success
    assert_output --partial "Added dependency"
    run read_field "$a" "deps"
    assert_output --partial "$b"
}

@test "dep: first dep updates empty array" {
    local a b
    a=$(create_ticket "Empty deps")
    b=$(create_ticket "Dep target")
    run read_field "$a" "deps"
    assert_output "[]"
    "$TK" dep "$a" "$b" > /dev/null
    run read_field "$a" "deps"
    assert_output --partial "$b"
}

@test "dep: multiple deps appended" {
    local a b c
    a=$(create_ticket "Multi dep")
    b=$(create_ticket "Dep 1")
    c=$(create_ticket "Dep 2")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$a" "$c" > /dev/null
    run read_field "$a" "deps"
    assert_output --partial "$b"
    assert_output --partial "$c"
}

@test "dep: duplicate is idempotent" {
    local a b
    a=$(create_ticket "Dup dep")
    b=$(create_ticket "Dep")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" dep "$a" "$b"
    assert_success
    assert_output --partial "already exists"
}

@test "dep: both IDs must resolve" {
    local a
    a=$(create_ticket "Real ticket")
    run "$TK" dep "$a" "nonexistent-0000"
    assert_failure
}

@test "dep: nonexistent source fails" {
    local b
    b=$(create_ticket "Target")
    run "$TK" dep "nonexistent-0000" "$b"
    assert_failure
}

# --- undep ---

@test "undep: removes dependency" {
    local a b
    a=$(create_ticket "Has dep")
    b=$(create_ticket "Was dep")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" undep "$a" "$b"
    assert_success
    run read_field "$a" "deps"
    # Should be empty or not contain b
    refute_output --partial "$b"
}

@test "undep: normalizes to empty array" {
    local a b
    a=$(create_ticket "Single dep")
    b=$(create_ticket "Only dep")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" undep "$a" "$b" > /dev/null
    run read_field "$a" "deps"
    assert_output "[]"
}

@test "undep: preserves remaining deps" {
    local a b c
    a=$(create_ticket "Multi")
    b=$(create_ticket "Keep")
    c=$(create_ticket "Remove")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$a" "$c" > /dev/null
    "$TK" undep "$a" "$c" > /dev/null
    run read_field "$a" "deps"
    assert_output --partial "$b"
    refute_output --partial "$c"
}

@test "dep: does not prevent cycles" {
    local a b
    a=$(create_ticket "Cycle A")
    b=$(create_ticket "Cycle B")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" dep "$b" "$a"
    assert_success
}
