#!/usr/bin/env bats
# Tests for: tk closed (Spec 7.15)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "closed: shows closed tickets" {
    local id
    id=$(create_ticket "Done ticket")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" closed
    assert_success
    assert_output --partial "Done ticket"
}

@test "closed: excludes open tickets" {
    create_ticket "Open ticket" > /dev/null
    local id
    id=$(create_ticket "Closed ticket")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" closed
    assert_success
    assert_output --partial "Closed ticket"
    refute_output --partial "Open ticket"
}

@test "closed: default limit is 20" {
    # Create 25 closed tickets
    for i in $(seq 1 25); do
        local id
        id=$(create_ticket "Closed ${i}")
        "$TK" edit "$id" --status closed > /dev/null
    done
    run "$TK" closed
    assert_success
    # Count non-header output lines
    local count
    count=$(echo "$output" | grep -c "\[closed\]")
    [[ "$count" -le 20 ]]
}

@test "closed: --limit restricts results" {
    for i in $(seq 1 5); do
        local id
        id=$(create_ticket "Closed ${i}")
        "$TK" edit "$id" --status closed > /dev/null
    done
    run "$TK" closed --limit=2
    assert_success
    local count
    count=$(echo "$output" | grep -c "\[closed\]")
    [[ "$count" -le 2 ]]
}

@test "closed: filter by assignee" {
    local a b
    a=$(create_ticket "Alice done" -a "alice")
    b=$(create_ticket "Bob done" -a "bob")
    "$TK" edit "$a" --status closed > /dev/null
    "$TK" edit "$b" --status closed > /dev/null
    run "$TK" closed -a alice
    assert_success
    assert_output --partial "Alice done"
    refute_output --partial "Bob done"
}

@test "closed: filter by tag" {
    local id
    id=$(create_ticket "Tagged closed" --tags "backend")
    "$TK" edit "$id" --status closed > /dev/null
    local id2
    id2=$(create_ticket "Untagged closed")
    "$TK" edit "$id2" --status closed > /dev/null
    run "$TK" closed -T backend
    assert_success
    assert_output --partial "Tagged closed"
    refute_output --partial "Untagged closed"
}

@test "closed: ordered by mtime (most recent first)" {
    local a b
    a=$(create_ticket "First closed")
    "$TK" edit "$a" --status closed > /dev/null
    sleep 1
    b=$(create_ticket "Second closed")
    "$TK" edit "$b" --status closed > /dev/null
    run "$TK" closed
    assert_success
    # Second should appear before First (more recent mtime)
    local second_line first_line
    second_line=$(echo "$output" | grep -n "Second closed" | head -1 | cut -d: -f1)
    first_line=$(echo "$output" | grep -n "First closed" | head -1 | cut -d: -f1)
    [[ "$second_line" -lt "$first_line" ]]
}

@test "closed: matches legacy done status" {
    write_raw_ticket "legacy-0001" "id: legacy-0001
status: done
deps: []
links: []
created: 2026-01-15T10:00:00Z
type: task
priority: 2" "# Legacy done ticket"
    run "$TK" closed
    assert_success
    assert_output --partial "Legacy done ticket"
}

@test "closed: unknown flags silently ignored" {
    local id
    id=$(create_ticket "Ignore flag")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" closed --bogus
    assert_success
}
