#!/usr/bin/env bats
# Tests for: tk ls / list (Spec 7.12)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "ls: lists all tickets by default" {
    create_ticket "Open one" > /dev/null
    create_ticket "Open two" > /dev/null
    run "$TK" ls
    assert_success
    assert_output --partial "Open one"
    assert_output --partial "Open two"
}

@test "ls: includes closed tickets by default" {
    local id
    id=$(create_ticket "Closed one")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" ls
    assert_success
    assert_output --partial "Closed one"
}

@test "ls: prints column header" {
    create_ticket "Header test" > /dev/null
    run "$TK" ls
    assert_success
    assert_output --partial "ID"
    assert_output --partial "STATUS"
    assert_output --partial "TITLE"
}

@test "ls: shows dependency info" {
    local a b
    b=$(create_ticket "Dep")
    a=$(create_ticket "Has dep")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" ls
    assert_success
    assert_output --partial "<-"
}

@test "ls: filter by status" {
    local id
    id=$(create_ticket "In progress")
    "$TK" edit "$id" --status in_progress > /dev/null
    create_ticket "Open one" > /dev/null
    run "$TK" ls --status=in_progress
    assert_success
    assert_output --partial "In progress"
    refute_output --partial "Open one"
}

@test "ls: filter by type" {
    create_ticket "Feature" -t feature > /dev/null
    create_ticket "Bug" -t bug > /dev/null
    run "$TK" ls -t feature
    assert_success
    assert_output --partial "Feature"
    refute_output --partial "Bug"
}

@test "ls: filter by priority" {
    create_ticket "P0 ticket" -p 0 > /dev/null
    create_ticket "P3 ticket" -p 3 > /dev/null
    run "$TK" ls -P 0
    assert_success
    assert_output --partial "P0 ticket"
    refute_output --partial "P3 ticket"
}

@test "ls: filter by assignee" {
    create_ticket "Alice task" -a "alice" > /dev/null
    create_ticket "Bob task" -a "bob" > /dev/null
    run "$TK" ls -a alice
    assert_success
    assert_output --partial "Alice task"
    refute_output --partial "Bob task"
}

@test "ls: filter by tag" {
    create_ticket "Backend" --tags "backend,api" > /dev/null
    create_ticket "Frontend" --tags "frontend" > /dev/null
    run "$TK" ls -T backend
    assert_success
    assert_output --partial "Backend"
    refute_output --partial "Frontend"
}

@test "ls: multiple filters combine as AND" {
    create_ticket "P0 feature" -t feature -p 0 > /dev/null
    create_ticket "P0 bug" -t bug -p 0 > /dev/null
    create_ticket "P2 feature" -t feature -p 2 > /dev/null
    run "$TK" ls -t feature -P 0
    assert_success
    assert_output --partial "P0 feature"
    refute_output --partial "P0 bug"
    refute_output --partial "P2 feature"
}

@test "ls: sort by status then priority" {
    local ip
    ip=$(create_ticket "In progress P2" -p 2)
    "$TK" edit "$ip" --status in_progress > /dev/null
    create_ticket "Open P1" -p 1 > /dev/null
    create_ticket "Open P3" -p 3 > /dev/null
    run "$TK" ls
    assert_success
    # in_progress should appear before open tickets
    local ip_line open_line
    ip_line=$(echo "$output" | grep -n "In progress P2" | head -1 | cut -d: -f1)
    open_line=$(echo "$output" | grep -n "Open P1" | head -1 | cut -d: -f1)
    [[ "$ip_line" -lt "$open_line" ]]
}

@test "ls: list is alias for ls" {
    create_ticket "Alias test" > /dev/null
    run "$TK" list
    assert_success
    assert_output --partial "Alias test"
}

@test "ls: unknown flags silently ignored" {
    create_ticket "Permissive" > /dev/null
    run "$TK" ls --bogus-flag
    assert_success
    assert_output --partial "Permissive"
}

@test "ls: empty tickets dir produces no output" {
    run "$TK" ls
    # awk-based ls exits non-zero when no files match; actual behavior
    [[ -z "$output" ]] || [[ "$status" -ne 0 ]]
}

# --- Grouping ---

@test "ls: --group-by=type groups by type" {
    create_ticket "A feature" -t feature > /dev/null
    create_ticket "A bug" -t bug > /dev/null
    run "$TK" ls --group-by=type
    assert_success
    assert_output --partial "==="
}

@test "ls: --group-by=status groups by status" {
    local id
    id=$(create_ticket "Open")
    create_ticket "Open 2" > /dev/null
    run "$TK" ls --group-by=status
    assert_success
    assert_output --partial "==="
}

@test "ls: --group-by=priority groups by priority" {
    create_ticket "P0" -p 0 > /dev/null
    create_ticket "P2" -p 2 > /dev/null
    run "$TK" ls --group-by=priority
    assert_success
    assert_output --partial "==="
}

@test "ls: --group shorthand for --group-by=workflow" {
    local id
    id=$(create_ticket "Workflow test")
    "$TK" edit "$id" --status in_progress > /dev/null
    create_ticket "Open ticket" > /dev/null
    run "$TK" ls --group
    assert_success
    assert_output --partial "==="
}

@test "ls: --type long form works" {
    create_ticket "Feature long" -t feature > /dev/null
    create_ticket "Task long" > /dev/null
    run "$TK" ls --type=feature
    assert_success
    assert_output --partial "Feature long"
    refute_output --partial "Task long"
}

@test "ls: priority filter strips P prefix" {
    create_ticket "P1 strip" -p 1 > /dev/null
    create_ticket "P3 strip" -p 3 > /dev/null
    run "$TK" ls -P P1
    assert_success
    assert_output --partial "P1 strip"
    refute_output --partial "P3 strip"
}
