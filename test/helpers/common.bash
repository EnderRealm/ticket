#!/usr/bin/env bash
# Common BATS test helpers for tk test suite

# Resolve project root (two levels up from helpers/)
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TK="${PROJECT_ROOT}/ticket"

# Load BATS libraries
load "${PROJECT_ROOT}/test/test_helper/bats-support/load"
load "${PROJECT_ROOT}/test/test_helper/bats-assert/load"
load "${PROJECT_ROOT}/test/test_helper/bats-file/load"

# Create a fresh, isolated tickets directory for each test.
# Sets TICKETS_DIR to a temp dir inside BATS_TEST_TMPDIR (auto-cleaned by BATS).
# Disables color output for deterministic assertions.
setup_fresh_tickets_dir() {
    export TICKETS_DIR="${BATS_TEST_TMPDIR}/tickets"
    mkdir -p "$TICKETS_DIR"
    export NO_COLOR=1
}

# Assert a YAML frontmatter field has an expected value.
# Usage: assert_yaml_field <file> <field> <expected>
assert_yaml_field() {
    local file="$1" field="$2" expected="$3"
    local actual
    actual=$(awk -v f="$field" '
        /^---$/ { fm++; next }
        fm == 1 && $0 ~ "^"f": " { sub("^"f": *", ""); print; exit }
    ' "$file")
    if [[ "$actual" != "$expected" ]]; then
        echo "Field '${field}' in ${file}:"
        echo "  expected: ${expected}"
        echo "  actual:   ${actual}"
        return 1
    fi
}

# Assert a command completes within a time budget (seconds).
# Usage: assert_perf <seconds> <command> [args...]
assert_perf() {
    local budget="$1"
    shift
    local start end elapsed
    start=$(date +%s)
    "$@"
    end=$(date +%s)
    elapsed=$(( end - start ))
    if (( elapsed > budget )); then
        echo "Performance budget exceeded: ${elapsed}s > ${budget}s"
        echo "Command: $*"
        return 1
    fi
}
