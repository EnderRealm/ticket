#!/usr/bin/env bats
# Tests for: Ticket file format (Spec 5.4-5.7)

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    setup_fresh_tickets_dir
}

@test "format: file starts with YAML frontmatter delimiters" {
    local id
    id=$(create_ticket "Format test")
    local file="${TICKETS_DIR}/${id}.md"
    local first_line
    first_line=$(head -1 "$file")
    [[ "$first_line" == "---" ]]
}

@test "format: frontmatter ends with second delimiter" {
    local id
    id=$(create_ticket "Delimiters")
    local file="${TICKETS_DIR}/${id}.md"
    local count
    count=$(grep -c "^---$" "$file")
    [[ "$count" -ge 2 ]]
}

@test "format: id field present in frontmatter" {
    local id
    id=$(create_ticket "ID field")
    assert_yaml_field "${TICKETS_DIR}/${id}.md" "id" "$id"
}

@test "format: status field present" {
    local id
    id=$(create_ticket "Status field")
    assert_yaml_field "${TICKETS_DIR}/${id}.md" "status" "open"
}

@test "format: deps field present as array" {
    local id
    id=$(create_ticket "Deps field")
    run read_field "$id" "deps"
    assert_output "[]"
}

@test "format: links field present as array" {
    local id
    id=$(create_ticket "Links field")
    run read_field "$id" "links"
    assert_output "[]"
}

@test "format: created field is ISO 8601 UTC" {
    local id
    id=$(create_ticket "Created field")
    run read_field "$id" "created"
    # YYYY-MM-DDTHH:MM:SSZ
    [[ "$output" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]
}

@test "format: type field present" {
    local id
    id=$(create_ticket "Type field")
    run read_field "$id" "type"
    assert_output "task"
}

@test "format: priority field present as integer" {
    local id
    id=$(create_ticket "Priority field")
    run read_field "$id" "priority"
    [[ "$output" =~ ^[0-4]$ ]]
}

@test "format: title is first h1 after frontmatter" {
    local id
    id=$(create_ticket "My Title")
    local file="${TICKETS_DIR}/${id}.md"
    # Title line should start with "# "
    grep -q "^# My Title" "$file"
}

@test "format: file stored as .md in TICKETS_DIR" {
    local id
    id=$(create_ticket "File location")
    [[ -f "${TICKETS_DIR}/${id}.md" ]]
}
