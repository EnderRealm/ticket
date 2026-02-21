#!/usr/bin/env bats
# Tests for: Partial ID resolution (Spec 5.3)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "partial-id: exact match resolves" {
    local id
    id=$(create_ticket "Exact match")
    run "$TK" show "$id"
    assert_success
    assert_output --partial "Exact match"
}

@test "partial-id: substring match resolves" {
    local id
    id=$(create_ticket "Substring match")
    # Use last 3 chars of ID as partial
    local partial="${id: -3}"
    run "$TK" show "$partial"
    assert_success
    assert_output --partial "Substring match"
}

@test "partial-id: ambiguous match fails" {
    # Create two tickets, then try a partial that could match both
    # This is probabilistic — we rely on the prefix being the same
    local id1 id2
    id1=$(create_ticket "Ambig one")
    id2=$(create_ticket "Ambig two")
    # Use just the prefix (before the dash) which should be the same for both
    local prefix="${id1%%-*}"
    run "$TK" show "$prefix"
    # If both IDs share the prefix, this should be ambiguous
    if [[ "$id1" == "${prefix}"* ]] && [[ "$id2" == "${prefix}"* ]]; then
        assert_failure
        assert_output --partial "ambiguous"
    else
        # If prefix happens to be unique to one, that's also valid
        assert_success
    fi
}

@test "partial-id: zero matches fails" {
    create_ticket "Existing" > /dev/null
    run "$TK" show "zzz-nonexistent"
    assert_failure
    assert_output --partial "not found"
}

@test "partial-id: works with edit command" {
    local id
    id=$(create_ticket "Edit partial")
    local partial="${id: -3}"
    run "$TK" edit "$partial" --status in_progress
    assert_success
}

@test "partial-id: works with delete command" {
    local id
    id=$(create_ticket "Delete partial")
    local partial="${id: -3}"
    run "$TK" delete "$partial"
    assert_success
    [[ ! -f "${TICKETS_DIR}/${id}.md" ]]
}

@test "partial-id: works with add-note command" {
    local id
    id=$(create_ticket "Note partial")
    local partial="${id: -3}"
    run "$TK" add-note "$partial" "Partial ID note"
    assert_success
    run "$TK" show "$id"
    assert_output --partial "Partial ID note"
}

@test "partial-id: works with dep command" {
    local a b
    a=$(create_ticket "Dep source")
    b=$(create_ticket "Dep target")
    local partial_b="${b: -3}"
    run "$TK" dep "$a" "$partial_b"
    assert_success
}
