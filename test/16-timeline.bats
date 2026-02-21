#!/usr/bin/env bats
# Tests for: tk timeline (Spec 7.18)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "timeline: shows header" {
    local id
    id=$(create_ticket "Timeline test")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" timeline
    assert_success
    assert_output --partial "TICKETS CLOSED BY WEEK"
}

@test "timeline: no closed tickets prints message" {
    create_ticket "Open only" > /dev/null
    run "$TK" timeline
    assert_success
    assert_output --partial "No closed tickets found"
}

@test "timeline: shows bar characters" {
    local id
    id=$(create_ticket "Bar test")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" timeline
    assert_success
    # Should have block chars
    [[ "$output" =~ █ ]]
}

@test "timeline: shows week labels" {
    local id
    id=$(create_ticket "Week label")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" timeline
    assert_success
    # Week label format: YYYY-WNN
    [[ "$output" =~ [0-9]{4}-W[0-9]{2} ]]
}

@test "timeline: --weeks controls display range" {
    local id
    id=$(create_ticket "Weeks test")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" timeline --weeks=2
    assert_success
    # Should work with custom week count
    assert_output --partial "TICKETS CLOSED BY WEEK"
}

@test "timeline: unknown flags silently ignored" {
    local id
    id=$(create_ticket "Permissive")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" timeline --bogus
    assert_success
}

@test "timeline: shows count after bars" {
    local id
    id=$(create_ticket "Count test")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" timeline
    assert_success
    # Should have a number after the bar
    [[ "$output" =~ █+[[:space:]]+[0-9]+ ]]
}
