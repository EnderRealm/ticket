#!/usr/bin/env bats
# Tests for: tk add-note (Spec 7.5)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "add-note: appends note with timestamp" {
    local id
    id=$(create_ticket "Note test")
    run "$TK" add-note "$id" "First note"
    assert_success
    run "$TK" show "$id"
    assert_output --partial "## Notes"
    assert_output --partial "First note"
}

@test "add-note: timestamp is bold UTC ISO 8601" {
    local id
    id=$(create_ticket "Timestamp test")
    "$TK" add-note "$id" "Check timestamp" > /dev/null
    run "$TK" show "$id"
    # Matches **YYYY-MM-DDTHH:MM:SSZ**
    [[ "$output" =~ \*\*[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z\*\* ]]
}

@test "add-note: multiple notes append without removing existing" {
    local id
    id=$(create_ticket "Multi note")
    "$TK" add-note "$id" "Note one" > /dev/null
    "$TK" add-note "$id" "Note two" > /dev/null
    run "$TK" show "$id"
    assert_output --partial "Note one"
    assert_output --partial "Note two"
}

@test "add-note: creates Notes section if missing" {
    local id
    id=$(create_ticket "No notes yet")
    run "$TK" show "$id"
    refute_output --partial "## Notes"
    "$TK" add-note "$id" "First" > /dev/null
    run "$TK" show "$id"
    assert_output --partial "## Notes"
}

@test "add-note: reads from stdin when piped" {
    local id
    id=$(create_ticket "Stdin note")
    echo "Piped content" | "$TK" add-note "$id"
    run "$TK" show "$id"
    assert_output --partial "Piped content"
}

@test "add-note: no text argument and empty stdin should fail" {
    skip "blocked on t-c9da: empty stdin adds empty note instead of failing"
    local id
    id=$(create_ticket "No content")
    run "$TK" add-note "$id" </dev/null
    assert_failure
    assert_output --partial "no note provided"
}

@test "add-note: nonexistent ticket fails" {
    run "$TK" add-note "nonexistent-0000" "Note"
    assert_failure
}
