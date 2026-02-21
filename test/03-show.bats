#!/usr/bin/env bats
# Tests for: tk show (Spec 7.3)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "show: displays ticket content" {
    local id
    id=$(create_ticket "Show test" -d "Description here")
    run "$TK" show "$id"
    assert_success
    assert_output --partial "# Show test"
    assert_output --partial "Description here"
}

@test "show: displays frontmatter" {
    local id
    id=$(create_ticket "Frontmatter" -t feature -p 1)
    run "$TK" show "$id"
    assert_success
    assert_output --partial "id: $id"
    assert_output --partial "type: feature"
    assert_output --partial "priority: 1"
}

@test "show: decorates parent with title comment" {
    local epic child
    epic=$(create_ticket "My Epic" -t epic)
    child=$(create_ticket "Child" --parent "$epic")
    run "$TK" show "$child"
    assert_success
    assert_output --partial "parent: ${epic}"
    assert_output --partial "My Epic"
}

@test "show: appends Blockers section for unresolved deps" {
    local a b
    b=$(create_ticket "Blocker")
    a=$(create_ticket "Blocked")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" show "$a"
    assert_success
    assert_output --partial "## Blockers"
    assert_output --partial "$b"
}

@test "show: omits Blockers when all deps are closed" {
    local a b
    b=$(create_ticket "Dep done")
    a=$(create_ticket "No longer blocked")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" edit "$b" --status closed > /dev/null
    run "$TK" show "$a"
    assert_success
    refute_output --partial "## Blockers"
}

@test "show: appends Blocking section for reverse deps" {
    local a b
    b=$(create_ticket "Dependency")
    a=$(create_ticket "Dependent")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" show "$b"
    assert_success
    assert_output --partial "## Blocking"
    assert_output --partial "$a"
}

@test "show: appends Children section" {
    local epic child
    epic=$(create_ticket "Parent" -t epic)
    child=$(create_ticket "Child" --parent "$epic")
    run "$TK" show "$epic"
    assert_success
    assert_output --partial "## Children"
    assert_output --partial "$child"
}

@test "show: omits Children when no children exist" {
    local id
    id=$(create_ticket "Lone ticket")
    run "$TK" show "$id"
    assert_success
    refute_output --partial "## Children"
}

@test "show: appends Linked section" {
    local a b
    a=$(create_ticket "Linked A")
    b=$(create_ticket "Linked B")
    "$TK" link "$a" "$b" > /dev/null
    run "$TK" show "$a"
    assert_success
    assert_output --partial "## Linked"
    assert_output --partial "$b"
}

@test "show: omits Linked when no links" {
    local id
    id=$(create_ticket "No links")
    run "$TK" show "$id"
    assert_success
    refute_output --partial "## Linked"
}

@test "show: nonexistent ticket fails" {
    run "$TK" show "nonexistent-0000"
    assert_failure
}

@test "show: computed section format is dash-id-bracket-status-title" {
    local epic child
    epic=$(create_ticket "Format test" -t epic)
    child=$(create_ticket "Child ticket" --parent "$epic")
    run "$TK" show "$epic"
    assert_success
    # Format: - <id> [<status>] <title>
    assert_output --partial "- ${child} [open] Child ticket"
}

@test "show: Blocking section shows non-closed dependents only" {
    local dep t1 t2
    dep=$(create_ticket "Shared dep")
    t1=$(create_ticket "Open dependent")
    t2=$(create_ticket "Closed dependent")
    "$TK" dep "$t1" "$dep" > /dev/null
    "$TK" dep "$t2" "$dep" > /dev/null
    "$TK" edit "$t2" --status closed > /dev/null
    run "$TK" show "$dep"
    assert_success
    assert_output --partial "$t1"
    refute_output --partial "- ${t2}"
}
