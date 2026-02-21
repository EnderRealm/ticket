#!/usr/bin/env bats
# Tests for: Output formats (Spec 11)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "format: ls output has ID TYPE STATUS TITLE columns" {
    create_ticket "Format ls" -t feature > /dev/null
    run "$TK" ls
    assert_success
    # Header row
    assert_output --partial "ID"
    assert_output --partial "TYPE"
    assert_output --partial "STATUS"
    assert_output --partial "TITLE"
    # Data row should contain ticket info
    assert_output --partial "feature"
    assert_output --partial "Format ls"
}

@test "format: ls shows priority as Pn" {
    create_ticket "P1 format" -p 1 > /dev/null
    run "$TK" ls
    assert_success
    assert_output --partial "P1"
}

@test "format: ls deps shown as <- [ids]" {
    local a b
    b=$(create_ticket "Dep target")
    a=$(create_ticket "Has dep")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" ls
    assert_success
    assert_output --partial "<-"
}

@test "format: ready output is ID [Pn][status] - title" {
    create_ticket "Ready format" -p 1 > /dev/null
    run "$TK" ready
    assert_success
    [[ "$output" =~ \[P1\]\[open\]\ -\ Ready\ format ]]
}

@test "format: blocked output includes <- [blocker IDs]" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Blocked format")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" blocked
    assert_success
    assert_output --partial "<-"
    assert_output --partial "$b"
}

@test "format: closed output is ID [status] - title" {
    local id
    id=$(create_ticket "Closed format")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" closed
    assert_success
    [[ "$output" =~ \[closed\]\ -\ Closed\ format ]]
}

@test "format: dep tree uses box-drawing characters" {
    local a b
    a=$(create_ticket "Tree root")
    b=$(create_ticket "Tree dep")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" dep tree "$a"
    assert_success
    [[ "$output" =~ (├──|└──) ]]
}

@test "format: dep tree root format is ID [status] title" {
    local id
    id=$(create_ticket "Root ticket")
    run "$TK" dep tree "$id"
    assert_success
    [[ "$output" =~ ^${id}\ \[open\]\ Root\ ticket ]]
}

@test "format: show computed sections use - ID [status] title" {
    local epic child
    epic=$(create_ticket "Show format" -t epic)
    child=$(create_ticket "Child format" --parent "$epic")
    run "$TK" show "$epic"
    assert_success
    assert_output --partial "- ${child} [open] Child format"
}
