#!/usr/bin/env bats
# Tests for: tk delete (Spec 7.4)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "delete: removes ticket file" {
    local id
    id=$(create_ticket "Delete me")
    run "$TK" delete "$id"
    assert_success
    assert_output --partial "Deleted"
    [[ ! -f "${TICKETS_DIR}/${id}.md" ]]
}

@test "delete: multiple IDs in one invocation" {
    local a b
    a=$(create_ticket "Delete A")
    b=$(create_ticket "Delete B")
    run "$TK" delete "$a" "$b"
    assert_success
    [[ ! -f "${TICKETS_DIR}/${a}.md" ]]
    [[ ! -f "${TICKETS_DIR}/${b}.md" ]]
}

@test "delete: prints Deleted for each" {
    local a b
    a=$(create_ticket "Del A")
    b=$(create_ticket "Del B")
    run "$TK" delete "$a" "$b"
    assert_success
    assert_output --partial "Deleted ${a}"
    assert_output --partial "Deleted ${b}"
}

@test "delete: nonexistent ticket prints error" {
    run "$TK" delete "nonexistent-0000"
    assert_output --partial "not found"
}

@test "delete: continues on partial failure" {
    local a
    a=$(create_ticket "Real ticket")
    run "$TK" delete "nonexistent-0000" "$a"
    # Should still delete the valid one
    [[ ! -f "${TICKETS_DIR}/${a}.md" ]]
}

@test "delete: does not clean up references in other tickets" {
    local a b
    a=$(create_ticket "Dep source")
    b=$(create_ticket "Dep target")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" delete "$b" > /dev/null
    # a still references deleted b in deps
    run read_field "$a" "deps"
    assert_output --partial "$b"
}
