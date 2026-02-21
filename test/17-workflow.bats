#!/usr/bin/env bats
# Tests for: tk workflow (Spec 7.19)

setup() {
    load 'helpers/common'
    setup_fresh_tickets_dir
}

@test "workflow: outputs static content" {
    run "$TK" workflow
    assert_success
    [[ -n "$output" ]]
}

@test "workflow: contains types section" {
    run "$TK" workflow
    assert_success
    assert_output --partial "type"
}

@test "workflow: contains statuses section" {
    run "$TK" workflow
    assert_success
    assert_output --partial "status"
}

@test "workflow: contains readiness rules" {
    run "$TK" workflow
    assert_success
    assert_output --partial "ready"
}

@test "workflow: contains propagation info" {
    run "$TK" workflow
    assert_success
    assert_output --partial "Propagation"
}

@test "workflow: contains commit format" {
    run "$TK" workflow
    assert_success
    assert_output --partial "commit"
}
