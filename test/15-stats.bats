#!/usr/bin/env bats
# Tests for: tk stats (Spec 7.17)
# BUG(t-b0f5): stats is broken on macOS — awk mktime() is gawk-only.
# These tests will fail on macOS until that's fixed.

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "stats: no tickets directory prints message" {
    export TICKETS_DIR="${BATS_TEST_TMPDIR}/nonexistent"
    run "$TK" stats
    assert_output --partial "No tickets directory"
}

@test "stats: shows PROJECT HEALTH header" {
    create_ticket "Stats test" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "PROJECT HEALTH"
}

@test "stats: shows status breakdown" {
    create_ticket "Open" > /dev/null
    local id
    id=$(create_ticket "Closed")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Status:"
    assert_output --partial "open"
    assert_output --partial "closed"
}

@test "stats: shows type breakdown" {
    create_ticket "Feature" -t feature > /dev/null
    create_ticket "Bug" -t bug > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Types:"
    assert_output --partial "feature"
    assert_output --partial "bug"
}

@test "stats: shows priority breakdown" {
    create_ticket "P0" -p 0 > /dev/null
    create_ticket "P2" -p 2 > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Priority:"
    assert_output --partial "P0"
    assert_output --partial "P2"
}

@test "stats: shows open ticket count" {
    create_ticket "One" > /dev/null
    create_ticket "Two" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Open Tickets:"
    assert_output --partial "Count"
}

@test "stats: shows TOTAL in status" {
    create_ticket "Total test" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "TOTAL"
}

@test "stats: counts are accurate" {
    create_ticket "A" > /dev/null
    create_ticket "B" > /dev/null
    create_ticket "C" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "3"
}

@test "stats: shows average age for open tickets" {
    create_ticket "Age test" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "age"
}
