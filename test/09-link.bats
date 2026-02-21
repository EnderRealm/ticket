#!/usr/bin/env bats
# Tests for: tk link and tk unlink (Spec 7.10, 7.11)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

# --- link ---

@test "link: creates symmetric links between two tickets" {
    local a b
    a=$(create_ticket "Link A")
    b=$(create_ticket "Link B")
    run "$TK" link "$a" "$b"
    assert_success
    run read_field "$a" "links"
    assert_output --partial "$b"
    run read_field "$b" "links"
    assert_output --partial "$a"
}

@test "link: all-to-all for three tickets" {
    local a b c
    a=$(create_ticket "A")
    b=$(create_ticket "B")
    c=$(create_ticket "C")
    run "$TK" link "$a" "$b" "$c"
    assert_success
    # Each should reference the other two
    run read_field "$a" "links"
    assert_output --partial "$b"
    assert_output --partial "$c"
    run read_field "$b" "links"
    assert_output --partial "$a"
    assert_output --partial "$c"
    run read_field "$c" "links"
    assert_output --partial "$a"
    assert_output --partial "$b"
}

@test "link: idempotent re-link" {
    local a b
    a=$(create_ticket "Idem A")
    b=$(create_ticket "Idem B")
    "$TK" link "$a" "$b" > /dev/null
    run "$TK" link "$a" "$b"
    assert_success
    assert_output --partial "already exist"
}

@test "link: requires at least two IDs" {
    local a
    a=$(create_ticket "Solo")
    run "$TK" link "$a"
    assert_failure
}

@test "link: reports count of links added" {
    local a b
    a=$(create_ticket "Count A")
    b=$(create_ticket "Count B")
    run "$TK" link "$a" "$b"
    assert_success
    # Should mention links added or similar
    [[ "$output" =~ (link|added|Added) ]]
}

@test "link: fails if any ID cannot resolve" {
    local a
    a=$(create_ticket "Valid")
    run "$TK" link "$a" "nonexistent-0000"
    assert_failure
}

# --- unlink ---

@test "unlink: removes link from both sides" {
    local a b
    a=$(create_ticket "Unlink A")
    b=$(create_ticket "Unlink B")
    "$TK" link "$a" "$b" > /dev/null
    run "$TK" unlink "$a" "$b"
    assert_success
    run read_field "$a" "links"
    refute_output --partial "$b"
    run read_field "$b" "links"
    refute_output --partial "$a"
}

@test "unlink: missing link fails" {
    local a b
    a=$(create_ticket "No link A")
    b=$(create_ticket "No link B")
    run "$TK" unlink "$a" "$b"
    assert_failure
    assert_output --partial "not found"
}

@test "unlink: prints confirmation" {
    local a b
    a=$(create_ticket "Conf A")
    b=$(create_ticket "Conf B")
    "$TK" link "$a" "$b" > /dev/null
    run "$TK" unlink "$a" "$b"
    assert_success
    assert_output --partial "Removed link"
}
