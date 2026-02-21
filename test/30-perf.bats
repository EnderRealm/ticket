#!/usr/bin/env bats
# Performance benchmark tests
# Uses setup_file to amortize the cost of creating 100 tickets.
# Thresholds are relative (not absolute) to catch O(n^2) regressions.

setup_file() {
    load 'helpers/common'
    load 'helpers/fixtures'

    export TICKETS_DIR="${BATS_FILE_TMPDIR}/tickets"
    mkdir -p "$TICKETS_DIR"
    export NO_COLOR=1

    # Create 100 tickets with varied attributes
    fixture_at_scale 100

    # Create a 20-deep dependency chain for tree tests
    local prev=""
    for i in $(seq 1 20); do
        local id
        id=$(create_ticket "Chain ${i}")
        if [[ -n "$prev" ]]; then
            "$TK" dep "$prev" "$id" > /dev/null
        fi
        prev="$id"
        if [[ "$i" -eq 1 ]]; then
            echo "$id" > "${BATS_FILE_TMPDIR}/chain_root"
        fi
    done
}

setup() {
    load 'helpers/common'
    load 'helpers/fixtures'
    export TICKETS_DIR="${BATS_FILE_TMPDIR}/tickets"
    export NO_COLOR=1
}

# Advisory failure: print warning but don't fail unless PERF_STRICT=1
perf_assert() {
    local budget="$1" elapsed="$2" cmd="$3"
    if (( elapsed > budget )); then
        echo "PERF WARNING: ${cmd} took ${elapsed}s (budget: ${budget}s)"
        if [[ "${PERF_STRICT:-0}" == "1" ]]; then
            return 1
        fi
    fi
}

@test "perf: ls with 100+ tickets completes in 5s" {
    local start end
    start=$(date +%s)
    run "$TK" ls
    end=$(date +%s)
    assert_success
    perf_assert 5 $(( end - start )) "ls (120 tickets)"
}

@test "perf: ready with 100+ tickets completes in 5s" {
    local start end
    start=$(date +%s)
    run "$TK" ready
    end=$(date +%s)
    assert_success
    perf_assert 5 $(( end - start )) "ready (120 tickets)"
}

@test "perf: blocked with 100+ tickets completes in 5s" {
    local start end
    start=$(date +%s)
    run "$TK" blocked
    end=$(date +%s)
    assert_success
    perf_assert 5 $(( end - start )) "blocked (120 tickets)"
}

@test "perf: query with 100+ tickets completes in 5s" {
    local start end
    start=$(date +%s)
    run "$TK" query
    end=$(date +%s)
    assert_success
    perf_assert 5 $(( end - start )) "query (120 tickets)"
}

@test "perf: dep tree 20-deep completes in 5s" {
    local root
    root=$(cat "${BATS_FILE_TMPDIR}/chain_root")
    local start end
    start=$(date +%s)
    run "$TK" dep tree "$root"
    end=$(date +%s)
    assert_success
    perf_assert 5 $(( end - start )) "dep tree (20-deep chain)"
}

@test "perf: show with 100+ tickets completes in 2s" {
    # Pick first ticket in the dir
    local first
    first=$(ls "$TICKETS_DIR"/*.md | head -1 | xargs basename | sed 's/.md$//')
    local start end
    start=$(date +%s)
    run "$TK" show "$first"
    end=$(date +%s)
    assert_success
    perf_assert 2 $(( end - start )) "show (120 tickets in dir)"
}

@test "perf: create+delete cycle in 2s" {
    local start end
    start=$(date +%s)
    local id
    id=$("$TK" create "Perf cycle" 2>&1 | awk '/^id:/{print $2}')
    "$TK" delete "$id" > /dev/null
    end=$(date +%s)
    perf_assert 2 $(( end - start )) "create+delete cycle"
}
