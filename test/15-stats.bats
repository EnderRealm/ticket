#!/usr/bin/env bats
# Tests for: tk stats (Spec 7.17)
# Note: stats uses awk mktime() which requires gawk. Tests that invoke stats
# with ticket data are skipped on systems without gawk support.

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

# Check if awk supports mktime (gawk does, BSD awk does not)
has_mktime() {
    echo "" | awk 'BEGIN { mktime("2026 01 01 0 0 0") }' 2>/dev/null
}

@test "stats: no tickets directory prints message" {
    export TICKETS_DIR="${BATS_TEST_TMPDIR}/nonexistent"
    run "$TK" stats
    assert_output --partial "No tickets directory"
}

@test "stats: shows PROJECT HEALTH header" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "Stats test" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "PROJECT HEALTH"
}

@test "stats: shows status breakdown" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
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
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "Feature" -t feature > /dev/null
    create_ticket "Bug" -t bug > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Types:"
    assert_output --partial "feature"
    assert_output --partial "bug"
}

@test "stats: shows priority breakdown" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "P0" -p 0 > /dev/null
    create_ticket "P2" -p 2 > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Priority:"
    assert_output --partial "P0"
    assert_output --partial "P2"
}

@test "stats: shows open ticket count" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "One" > /dev/null
    create_ticket "Two" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "Open Tickets:"
    assert_output --partial "Count"
}

@test "stats: shows TOTAL in status" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "Total test" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "TOTAL"
}

@test "stats: counts are accurate" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "A" > /dev/null
    create_ticket "B" > /dev/null
    create_ticket "C" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "3"
}

@test "stats: shows average age for open tickets" {
    has_mktime || skip "awk lacks mktime (requires gawk)"
    create_ticket "Age test" > /dev/null
    run "$TK" stats
    assert_success
    assert_output --partial "age"
}
