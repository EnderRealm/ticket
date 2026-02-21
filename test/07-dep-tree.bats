#!/usr/bin/env bats
# Tests for: tk dep tree (Spec 7.8)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "dep tree: shows root ticket" {
    local id
    id=$(create_ticket "Root")
    run "$TK" dep tree "$id"
    assert_success
    assert_output --partial "$id"
    assert_output --partial "Root"
}

@test "dep tree: shows direct dependencies" {
    local a b
    a=$(create_ticket "Parent")
    b=$(create_ticket "Child dep")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" dep tree "$a"
    assert_success
    assert_output --partial "$b"
    assert_output --partial "Child dep"
}

@test "dep tree: shows transitive dependencies" {
    local a b c
    read -r a b c <<< "$(fixture_dep_chain)"
    run "$TK" dep tree "$a"
    assert_success
    assert_output --partial "$b"
    assert_output --partial "$c"
}

@test "dep tree: uses box-drawing characters" {
    local a b c
    a=$(create_ticket "Root")
    b=$(create_ticket "Dep 1")
    c=$(create_ticket "Dep 2")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$a" "$c" > /dev/null
    run "$TK" dep tree "$a"
    assert_success
    # Should have tree connectors
    [[ "$output" =~ (├──|└──) ]]
}

@test "dep tree: default mode deduplicates shared nodes" {
    local a b c shared
    shared=$(create_ticket "Shared")
    b=$(create_ticket "Branch 1")
    c=$(create_ticket "Branch 2")
    a=$(create_ticket "Root")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$a" "$c" > /dev/null
    "$TK" dep "$b" "$shared" > /dev/null
    "$TK" dep "$c" "$shared" > /dev/null
    run "$TK" dep tree "$a"
    assert_success
    # Shared should appear exactly once in default mode
    local count
    count=$(echo "$output" | grep -c "$shared")
    [[ "$count" -eq 1 ]]
}

@test "dep tree: --full shows all instances of shared nodes" {
    local a b c shared
    shared=$(create_ticket "Shared")
    b=$(create_ticket "Branch 1")
    c=$(create_ticket "Branch 2")
    a=$(create_ticket "Root")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$a" "$c" > /dev/null
    "$TK" dep "$b" "$shared" > /dev/null
    "$TK" dep "$c" "$shared" > /dev/null
    run "$TK" dep tree --full "$a"
    assert_success
    # Shared should appear twice in full mode
    local count
    count=$(echo "$output" | grep -c "$shared")
    [[ "$count" -eq 2 ]]
}

@test "dep tree: displays status in brackets" {
    local a b
    a=$(create_ticket "Root")
    b=$(create_ticket "Done dep")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" edit "$b" --status closed > /dev/null
    run "$TK" dep tree "$a"
    assert_success
    assert_output --partial "[closed]"
}

@test "dep tree: unknown root ID fails" {
    run "$TK" dep tree "nonexistent-0000"
    assert_failure
}

@test "dep tree: handles cycles without infinite recursion" {
    local a b
    a=$(create_ticket "Cycle A")
    b=$(create_ticket "Cycle B")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$a" > /dev/null
    run "$TK" dep tree "$a"
    assert_success
    # Should terminate and show both
    assert_output --partial "$a"
    assert_output --partial "$b"
}
