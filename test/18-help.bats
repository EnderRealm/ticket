#!/usr/bin/env bats
# Tests for: tk help and command dispatch (Spec 7.21, Sec 13)

setup() {
    load 'helpers/common'
    setup_fresh_tickets_dir
}

@test "help: displays usage info" {
    run "$TK" help
    assert_success
    assert_output --partial "ticket management CLI"
    assert_output --partial "Usage:"
}

@test "help: --help flag works" {
    run "$TK" --help
    assert_success
    assert_output --partial "Usage:"
}

@test "help: -h flag works" {
    run "$TK" -h
    assert_success
    assert_output --partial "Usage:"
}

@test "help: no arguments shows help" {
    run "$TK"
    assert_success
    assert_output --partial "Usage:"
}

@test "help: unknown command prints error to stderr" {
    run "$TK" nonexistent-command
    assert_failure
}

@test "help: lists all major commands" {
    run "$TK" help
    assert_success
    assert_output --partial "create"
    assert_output --partial "edit"
    assert_output --partial "show"
    assert_output --partial "delete"
    assert_output --partial "ls"
    assert_output --partial "ready"
    assert_output --partial "blocked"
    assert_output --partial "query"
    assert_output --partial "stats"
    assert_output --partial "timeline"
}
