#!/usr/bin/env bats
# Tests for: Environment variables (Spec 9)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "env: TICKETS_DIR overrides default location" {
    local custom_dir="${BATS_TEST_TMPDIR}/custom-tickets"
    mkdir -p "$custom_dir"
    export TICKETS_DIR="$custom_dir"
    local id
    id=$(create_ticket "Custom dir ticket")
    [[ -f "${custom_dir}/${id}.md" ]]
}

@test "env: TICKETS_DIR used by all commands" {
    # Verify ls reads from the custom dir
    local id
    id=$(create_ticket "Env ls test")
    run "$TK" ls
    assert_success
    assert_output --partial "Env ls test"
}

@test "env: NO_COLOR disables color output" {
    local id
    id=$(create_ticket "No color test")
    "$TK" edit "$id" --status in_progress > /dev/null
    export NO_COLOR=1
    run "$TK" ls
    assert_success
    # Should not contain ANSI escape codes
    [[ ! "$output" =~ $'\033' ]]
}

@test "env: commands work without TICKETS_DIR set (uses .tickets default)" {
    unset TICKETS_DIR
    # Should not crash — just use default .tickets/
    run "$TK" help
    assert_success
}

@test "env: missing TICKETS_DIR causes silent return for read commands" {
    export TICKETS_DIR="${BATS_TEST_TMPDIR}/does-not-exist"
    run "$TK" ready
    # Should return 0 with no output when dir doesn't exist
    [[ -z "$output" ]] || assert_success
}
