#!/usr/bin/env bats
# Tests for: Error handling (Spec 12)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

# --- Missing required arguments ---

@test "error: create with no title fails" {
    run "$TK" create "" </dev/null
    assert_failure
}

@test "error: edit with no id fails" {
    run "$TK" edit
    assert_failure
}

@test "error: show with no id fails" {
    run "$TK" show
    assert_failure
}

@test "error: delete with no id fails" {
    run "$TK" delete
    assert_failure
}

@test "error: add-note with no id fails" {
    run "$TK" add-note
    assert_failure
}

@test "error: dep with insufficient args fails" {
    run "$TK" dep
    assert_failure
}

# --- Invalid values ---

@test "error: invalid status value rejected" {
    local id
    id=$(create_ticket "Bad status")
    run "$TK" edit "$id" --status badvalue
    assert_failure
}

@test "error: invalid priority value rejected" {
    run "$TK" create "Bad priority" -p 99
    assert_failure
}

# --- Ticket not found ---

@test "error: show nonexistent ticket fails" {
    run "$TK" show "zzz-0000"
    assert_failure
    assert_output --partial "not found"
}

@test "error: edit nonexistent ticket fails" {
    run "$TK" edit "zzz-0000" --status open
    assert_failure
}

# --- Unknown command ---

@test "error: unknown command exits non-zero" {
    run "$TK" nonexistent-command
    assert_failure
}

# --- Strict vs permissive flag handling ---

@test "error: create rejects unknown flags (strict)" {
    run "$TK" create "Strict" --unknown-flag
    assert_failure
}

@test "error: edit rejects unknown flags (strict)" {
    local id
    id=$(create_ticket "Strict edit")
    run "$TK" edit "$id" --unknown-flag
    assert_failure
}

@test "error: ls accepts unknown flags (permissive)" {
    create_ticket "Permissive" > /dev/null
    run "$TK" ls --unknown-flag
    assert_success
}
