#!/usr/bin/env bash
# Test fixture helpers for tk test suite

# Create a ticket via tk and return only the ID.
# Usage: id=$(create_ticket "Title" [extra args...])
create_ticket() {
    local title="$1"
    shift
    local output
    output=$("$TK" create "$title" "$@" 2>&1)
    # Extract ID from the show output (first field of the id: line in frontmatter)
    echo "$output" | sed -n 's/^id: *//p'
}

# Read a YAML frontmatter field from a ticket file by ID.
# Usage: val=$(read_field <id> <field>)
read_field() {
    local id="$1" field="$2"
    local file="${TICKETS_DIR}/${id}.md"
    awk -v f="$field" '
        /^---$/ { fm++; next }
        fm == 1 && $0 ~ "^"f": " { sub("^"f": *", ""); print; exit }
    ' "$file"
}

# Write a raw ticket file directly (bypass tk create).
# Usage: write_raw_ticket <id> <frontmatter> <body>
write_raw_ticket() {
    local id="$1" frontmatter="$2" body="$3"
    cat > "${TICKETS_DIR}/${id}.md" <<EOF
---
${frontmatter}
---
${body}
EOF
}

# Set file modification time (for testing mtime-dependent commands like closed).
# Usage: set_mtime <id> <YYYYMMDDHHMMSS>
# Example: set_mtime "t-abc1" "202601151030"
set_mtime() {
    local id="$1" timestamp="$2"
    touch -t "$timestamp" "${TICKETS_DIR}/${id}.md"
}

# Create a dependency chain: A depends on B, B depends on C.
# Returns space-separated IDs: "id_a id_b id_c"
# Usage: read -r a b c <<< "$(fixture_dep_chain)"
fixture_dep_chain() {
    local c b a
    c=$(create_ticket "Task C")
    b=$(create_ticket "Task B")
    a=$(create_ticket "Task A")
    "$TK" dep "$a" "$b" > /dev/null
    "$TK" dep "$b" "$c" > /dev/null
    echo "$a $b $c"
}

# Create a parent-child hierarchy: epic > child1, child2 > grandchild (under child1).
# Returns space-separated IDs: "epic child1 child2 grandchild"
# Usage: read -r epic c1 c2 gc <<< "$(fixture_hierarchy)"
fixture_hierarchy() {
    local epic c1 c2 gc
    epic=$(create_ticket "Epic" -t epic)
    "$TK" edit "$epic" --status in_progress > /dev/null
    c1=$(create_ticket "Child 1" --parent "$epic")
    c2=$(create_ticket "Child 2" --parent "$epic")
    gc=$(create_ticket "Grandchild" --parent "$c1")
    echo "$epic $c1 $c2 $gc"
}

# Create N tickets with varied attributes for scale testing.
# Usage: fixture_at_scale <count>
fixture_at_scale() {
    local count="$1"
    local types=("task" "feature" "bug" "epic" "chore")
    local priorities=(0 1 2 3 4)
    local i
    for (( i = 0; i < count; i++ )); do
        local t="${types[$((i % ${#types[@]}))]}"
        local p="${priorities[$((i % ${#priorities[@]}))]}"
        create_ticket "Scale ticket ${i}" -t "$t" -p "$p" > /dev/null
    done
}
