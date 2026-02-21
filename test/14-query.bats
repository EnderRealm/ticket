#!/usr/bin/env bats
# Tests for: tk query (Spec 7.16, Sec 8)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "query: outputs JSONL for all tickets" {
    create_ticket "Query one" > /dev/null
    create_ticket "Query two" > /dev/null
    run "$TK" query
    assert_success
    # Each line should be valid JSON
    local count
    count=$(echo "$output" | wc -l | tr -d ' ')
    [[ "$count" -eq 2 ]]
}

@test "query: includes id and title fields" {
    local id
    id=$(create_ticket "JSON title")
    run "$TK" query
    assert_success
    assert_output --partial "\"id\":\"${id}\""
    assert_output --partial "\"title\":\"JSON title\""
}

@test "query: priority emitted as string not number" {
    create_ticket "Priority string" -p 1 > /dev/null
    run "$TK" query
    assert_success
    assert_output --partial "\"priority\":\"1\""
}

@test "query: arrays emitted as JSON arrays" {
    local a b
    a=$(create_ticket "Array test")
    b=$(create_ticket "Dep target")
    "$TK" dep "$a" "$b" > /dev/null
    run "$TK" query '.id == "'"$a"'"'
    assert_success
    # deps should be a JSON array containing b
    assert_output --partial "\"deps\":[\"${b}\"]"
}

@test "query: body sections become snake_case keys" {
    create_ticket "Section test" --design "Design content" --acceptance "AC content" > /dev/null
    run "$TK" query
    assert_success
    assert_output --partial "\"design\":"
    assert_output --partial "\"acceptance_criteria\":"
}

@test "query: frontmatter keys preserve hyphens" {
    create_ticket "Hyphen test" --external-ref "gh-99" > /dev/null
    run "$TK" query
    assert_success
    assert_output --partial "\"external-ref\":\"gh-99\""
}

@test "query: empty body sections omitted" {
    create_ticket "No sections" > /dev/null
    run "$TK" query
    assert_success
    refute_output --partial "\"design\""
    refute_output --partial "\"acceptance_criteria\""
}

@test "query: jq filter selects matching tickets" {
    create_ticket "Open task" > /dev/null
    local id
    id=$(create_ticket "Closed task")
    "$TK" edit "$id" --status closed > /dev/null
    run "$TK" query '.status == "open"'
    assert_success
    assert_output --partial "Open task"
    refute_output --partial "Closed task"
}

@test "query: JSON escapes special characters" {
    create_ticket 'Quote "test" ticket' > /dev/null
    run "$TK" query
    assert_success
    # Title with embedded quotes should be in valid JSON
    # The raw output should contain escaped quotes: \"
    echo "$output" | jq . > /dev/null 2>&1
    assert_output --partial "test"
}

@test "query: tags emitted as JSON array" {
    create_ticket "Tag query" --tags "ui,backend" > /dev/null
    run "$TK" query
    assert_success
    assert_output --partial "\"tags\":"
    assert_output --partial "\"ui\""
    assert_output --partial "\"backend\""
}

@test "query: description field present when description exists" {
    create_ticket "Desc query" -d "My description" > /dev/null
    run "$TK" query
    assert_success
    assert_output --partial "\"description\":"
    assert_output --partial "My description"
}

@test "query: compound jq filter works" {
    create_ticket "P1 feature" -t feature -p 1 > /dev/null
    create_ticket "P1 bug" -t bug -p 1 > /dev/null
    create_ticket "P3 feature" -t feature -p 3 > /dev/null
    run "$TK" query '.type == "feature" and .priority == "1"'
    assert_success
    assert_output --partial "P1 feature"
    refute_output --partial "P1 bug"
    refute_output --partial "P3 feature"
}
