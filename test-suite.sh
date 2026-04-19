#!/usr/bin/env bash
# Integration test suite for tk (v7+).
# Exercises the CLI against a fresh, isolated central store so the caller's
# real tickets are untouched. Builds ./tk from this repo.

set -uo pipefail

# ─── Setup ──────────────────────────────────────────────────────────────────

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TK_BIN="$REPO_ROOT/tk"

if [[ ! -x "$TK_BIN" ]]; then
    echo "Building tk binary..."
    (cd "$REPO_ROOT" && go build -o "$TK_BIN" .)
fi

TEST_HOME=$(mktemp -d)
CENTRAL_ROOT=$(mktemp -d)
PROJECT_DIR=$(mktemp -d)
export HOME="$TEST_HOME"

cleanup() {
    rm -rf "$TEST_HOME" "$CENTRAL_ROOT" "$PROJECT_DIR"
}
trap cleanup EXIT

cd "$PROJECT_DIR"
git init -q

tk() { "$TK_BIN" "$@"; }
export -f tk
export TK_BIN

tk init --central-root "$CENTRAL_ROOT" --project tktest --yes > /dev/null

# ─── Output helpers ─────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0
FAILED_TESTS=()

log_pass() {
    echo -e "${GREEN}✓${NC} $1"
    PASS=$((PASS + 1))
}

log_fail() {
    echo -e "${RED}✗${NC} $1"
    FAIL=$((FAIL + 1))
    FAILED_TESTS+=("$1")
}

log_section() {
    echo -e "\n${YELLOW}=== $1 ===${NC}"
}

extract_id() {
    grep "^id:" | awk '{print $2}'
}

assert_ok() {
    if eval "$1" > /dev/null 2>&1; then
        log_pass "$2"
    else
        log_fail "$2"
    fi
}

assert_fail() {
    if eval "$1" > /dev/null 2>&1; then
        log_fail "$2 (should have failed)"
    else
        log_pass "$2"
    fi
}

assert_contains() {
    local output
    output=$(eval "$1" 2>&1) || true
    if echo "$output" | grep -q "$2"; then
        log_pass "$3"
    else
        log_fail "$3 (expected '$2' in output)"
        echo "  Got: $output"
    fi
}

assert_not_contains() {
    local output
    output=$(eval "$1" 2>&1) || true
    if echo "$output" | grep -q "$2"; then
        log_fail "$3 (unexpected '$2' in output)"
    else
        log_pass "$3"
    fi
}

# ─── CREATE ─────────────────────────────────────────────────────────────────
log_section "TICKET CREATION"

ID1=$(tk create "Test Basic Ticket" | extract_id)
if [[ -n "$ID1" ]]; then
    log_pass "Create basic ticket: $ID1"
else
    log_fail "Create basic ticket"
fi

ID2=$(tk create "Test With Description" -d "This is a description" | extract_id)
assert_contains "tk show $ID2" "This is a description" "Create with description"

ID3=$(tk create "Test Full Options" \
    -d "Full description" \
    -t epic \
    -p 1 \
    --external-ref "GH-123" \
    --tags "test,automated" | extract_id)
assert_contains "tk show $ID3" "type: epic" "Create with type"
assert_contains "tk show $ID3" "priority: 1" "Create with priority"
assert_contains "tk show $ID3" "external-ref: GH-123" "Create with external-ref"
assert_contains "tk show $ID3" "tags:" "Create with tags"

for type in feature bug epic; do
    ID=$(tk create "Test $type type" -t "$type" | extract_id)
    assert_contains "tk show $ID" "type: $type" "Create type: $type"
done

# Default priority is 2.
DEFAULT_P=$(tk create "Test Default Priority" | extract_id)
assert_contains "tk show $DEFAULT_P" "priority: 2" "Default priority is 2"

# Parent linkage.
ID_CHILD=$(tk create "Test Child Ticket" --parent "$ID3" | extract_id)
assert_contains "tk show $ID_CHILD" "parent: $ID3" "Create with parent"
assert_contains "tk show $ID3" "$ID_CHILD" "Parent shows child"

# --set extra fields.
SET_ID=$(tk create "Test Set Field" --set "category=infra" | extract_id)
assert_contains "tk show $SET_ID --metadata" "category: infra" "Create with --set"

# ─── EDIT ───────────────────────────────────────────────────────────────────
log_section "TICKET EDITING"

assert_ok "tk edit $ID1 --title 'Updated Title'" "Edit title"
assert_contains "tk show $ID1" "# Updated Title" "Title was updated"

assert_ok "tk edit $ID1 -d 'New description text'" "Edit description"
assert_contains "tk show $ID1" "New description text" "Description was updated"

# All v7 statuses.
for status in backlog ready open done closed; do
    assert_ok "tk edit $ID1 --status $status" "Edit status to $status"
    assert_contains "tk show $ID1" "status: $status" "Status is $status"
done

assert_ok "tk edit $ID1 -t bug" "Edit type"
assert_contains "tk show $ID1" "type: bug" "Type was updated"

assert_ok "tk edit $ID1 -p 0" "Edit priority"
assert_contains "tk show $ID1" "priority: 0" "Priority was updated"

assert_ok "tk edit $ID1 --tags 'new,tags,here'" "Edit tags"
assert_contains "tk show $ID1" "tags:" "Tags were updated"

assert_ok "tk edit $ID1 --branch 'feature/test'" "Edit branch"
assert_contains "tk show $ID1 --metadata" "branch: feature/test" "Branch was updated"

assert_ok "tk edit $ID1 --external-ref 'GH-999'" "Edit external-ref"
assert_contains "tk show $ID1" "external-ref: GH-999" "External-ref was updated"

assert_ok "tk edit $ID1 --note 'Appended via --note'" "Edit with --note"
assert_contains "tk show $ID1" "Appended via --note" "Note appended via edit"

# --set key=value then remove.
assert_ok "tk edit $ID1 --set category=backend" "Edit with --set"
assert_contains "tk show $ID1 --metadata" "category: backend" "--set added field"
assert_ok "tk edit $ID1 --set category=" "Remove field via --set"
assert_not_contains "tk show $ID1 --metadata" "category:" "--set empty removed field"

# Rejections.
assert_fail "tk edit $ID1 --status invalid_status" "Reject invalid status"
assert_fail "tk edit $ID1 -p 99" "Reject invalid priority"
assert_fail "tk edit $ID1 -t chore" "Reject removed type (chore)"
assert_fail "tk edit" "Reject edit without id"

# ─── DEPENDENCIES ───────────────────────────────────────────────────────────
log_section "DEPENDENCIES"

DEP1=$(tk create "Dep Test 1" | extract_id)
DEP2=$(tk create "Dep Test 2" | extract_id)
DEP3=$(tk create "Dep Test 3" | extract_id)

assert_ok "tk dep $DEP2 $DEP1" "Add dependency"
assert_contains "tk show $DEP2" "deps:" "Dependency recorded"
assert_contains "tk show $DEP2" "$DEP1" "Dependency ID appears"

# Show Blockers section lists unresolved deps.
assert_contains "tk show $DEP2" "## Blockers" "Blockers section shows unresolved deps"

# Mark dep done; blockers should clear.
tk edit "$DEP1" --status done > /dev/null
assert_not_contains "tk show $DEP2" "## Blockers" "Blockers cleared when dep done"

# Remove dependency.
tk edit "$DEP1" --status backlog > /dev/null
assert_ok "tk undep $DEP2 $DEP1" "Remove dependency"
assert_not_contains "tk show $DEP2" "## Blockers" "Dependency removed"

# Dep tree.
tk dep "$DEP2" "$DEP1" > /dev/null
tk dep "$DEP3" "$DEP2" > /dev/null
assert_contains "tk dep tree $DEP3" "$DEP2" "Dep tree shows direct dep"
assert_contains "tk dep tree $DEP3" "$DEP1" "Dep tree shows transitive dep"
assert_ok "tk dep tree --full $DEP3" "Dep tree --full succeeds"
assert_contains "tk dep tree --full $DEP3" "$DEP1" "Dep tree --full shows transitive dep"

assert_fail "tk dep" "Reject dep without args"

# ─── LINKS ──────────────────────────────────────────────────────────────────
log_section "LINKS"

LINK1=$(tk create "Link Test 1" | extract_id)
LINK2=$(tk create "Link Test 2" | extract_id)

assert_ok "tk link $LINK1 $LINK2" "Add link"
assert_contains "tk show $LINK1" "$LINK2" "Link appears in first ticket"
assert_contains "tk show $LINK2" "$LINK1" "Link is symmetric"

assert_ok "tk unlink $LINK1 $LINK2" "Remove link"
assert_not_contains "tk show $LINK1" "## Linked" "Link removed from first ticket"

# ─── LISTING & FILTERING ────────────────────────────────────────────────────
log_section "LISTING AND FILTERING"

FILTER1=$(tk create "Filter Test Alpha" -t feature -p 1 --tags "frontend" | extract_id)
FILTER2=$(tk create "Filter Test Beta" -t bug -p 2 --tags "backend" | extract_id)
tk edit "$FILTER2" --status done > /dev/null

# Default `tk ls` hides backlog + done; move FILTER1 to open so it shows there.
tk edit "$FILTER1" --status open > /dev/null
assert_contains "tk ls" "$FILTER1" "ls shows open tickets by default"
assert_not_contains "tk ls" "$FILTER2" "ls default hides done tickets"

# Status filter.
assert_contains "tk ls --status=open" "$FILTER1" "Filter by status=open"
assert_not_contains "tk ls --status=open" "$FILTER2" "open filter excludes done"
assert_contains "tk ls --status=done" "$FILTER2" "Filter by status=done"

# Type filter (FILTER1 is open so default listing includes it).
assert_contains "tk ls -t feature" "$FILTER1" "Filter by type"
assert_not_contains "tk ls -t feature" "$FILTER2" "Type filter excludes others"

# Tag filter.
assert_contains "tk ls -T frontend" "$FILTER1" "Filter by tag"
assert_not_contains "tk ls -T frontend" "$FILTER2" "Tag filter excludes others"

# Priority filter.
assert_contains "tk ls -P 1" "$FILTER1" "Filter by priority"
assert_not_contains "tk ls -P 1" "$FILTER2" "Priority filter excludes others"

# Parent filter. Child is in backlog, so scope to --status=backlog.
PAR_EPIC=$(tk create "Parent Epic for ls" -t epic | extract_id)
PAR_CHILD=$(tk create "Parent Child for ls" --parent "$PAR_EPIC" | extract_id)
assert_contains "tk ls --parent=$PAR_EPIC --status=backlog" "$PAR_CHILD" "ls --parent shows child"
assert_not_contains "tk ls --parent=$PAR_EPIC --status=backlog" "$FILTER1" "ls --parent excludes non-children"

# Grouping.
assert_contains "tk ls --group-by=workflow" "===" "group-by=workflow has headers"
assert_contains "tk ls --group-by=type" "===" "group-by=type has headers"
assert_contains "tk ls --group-by=priority" "===" "group-by=priority has headers"
assert_ok "tk ls --flat" "ls --flat succeeds"
assert_fail "tk ls --group-by=invalid" "Reject invalid group-by"

# ─── PARTIAL ID MATCHING ────────────────────────────────────────────────────
log_section "PARTIAL ID MATCHING"

SUFFIX="${FILTER1##*-}"
assert_contains "tk show $SUFFIX" "$FILTER1" "Partial (suffix) ID resolves to full ticket"

# ─── SHOW MULTIPLE IDS ──────────────────────────────────────────────────────
log_section "SHOW MULTIPLE IDS"

MULTI_OUTPUT=$(tk show "$FILTER1" "$DEP1" 2>&1)
if echo "$MULTI_OUTPUT" | grep -q "$FILTER1" && echo "$MULTI_OUTPUT" | grep -q "$DEP1"; then
    log_pass "show accepts multiple IDs"
else
    log_fail "show accepts multiple IDs"
fi

# ─── SHOW --metadata ────────────────────────────────────────────────────────
log_section "SHOW --metadata"

META_OUTPUT=$(tk show "$FILTER1" --metadata 2>&1)
if echo "$META_OUTPUT" | grep -q "^id:"; then
    log_pass "show --metadata outputs frontmatter"
else
    log_fail "show --metadata outputs frontmatter"
fi

# ─── ADD-NOTE ───────────────────────────────────────────────────────────────
log_section "NOTES"

NOTE_ID=$(tk create "Note Test Ticket" | extract_id)

assert_ok "tk add-note $NOTE_ID 'This is a test note'" "add-note with arg"
assert_contains "tk show $NOTE_ID" "This is a test note" "Note appears in ticket"
assert_contains "tk show $NOTE_ID" "## Notes" "Notes section created"

assert_ok "tk add-note $NOTE_ID 'Second note'" "Add second note"
assert_contains "tk show $NOTE_ID" "Second note" "Second note appears"

STDIN_ID=$(tk create "Stdin Note Test" | extract_id)
echo "Note from stdin" | tk add-note "$STDIN_ID" > /dev/null
assert_contains "tk show $STDIN_ID" "Note from stdin" "add-note reads from stdin"

# ─── QUERY (JSON) ───────────────────────────────────────────────────────────
log_section "QUERY (JSON)"

assert_ok "tk query" "Query outputs JSON"
assert_contains "tk query" '"id":' "Query contains id field"
assert_contains "tk query" '"status":' "Query contains status field"

if command -v jq &> /dev/null; then
    assert_contains "tk query '.type == \"epic\"'" '"type":"epic"' "Query with jq filter"
fi

# ─── STATUS COMMAND ─────────────────────────────────────────────────────────
log_section "STATUS"

assert_contains "tk status" "central root" "status shows central root"
assert_contains "tk status" "tktest" "status shows registered project"

# ─── HELP ───────────────────────────────────────────────────────────────────
log_section "HELP"

assert_contains "tk help" "Usage:" "Help shows usage"
assert_contains "tk help" "create" "Help shows create command"
assert_contains "tk help" "edit" "Help shows edit command"
assert_contains "tk --help" "Usage:" "--help flag works"
assert_contains "tk -h" "Usage:" "-h flag works"

# ─── ERROR HANDLING ─────────────────────────────────────────────────────────
log_section "ERROR HANDLING"

assert_fail "tk unknown_command" "Reject unknown command"
assert_fail "tk delete" "Reject delete without id"
assert_fail "tk show nonexistent_id_xyz" "Reject nonexistent ticket"
assert_fail "tk create" "Reject create without title"
assert_fail "tk create 'No type' -t chore" "Reject removed type on create"

# ─── DELETE ─────────────────────────────────────────────────────────────────
log_section "DELETE"

DELETE_ID=$(tk create "To Be Deleted" | extract_id)
assert_ok "tk delete $DELETE_ID" "Delete ticket"
assert_fail "tk show $DELETE_ID" "Deleted ticket not found"

# ─── RESULTS ────────────────────────────────────────────────────────────────

echo ""
echo "========================================"
echo -e "  ${GREEN}PASSED: $PASS${NC}"
echo -e "  ${RED}FAILED: $FAIL${NC}"
echo "========================================"

if [[ $FAIL -gt 0 ]]; then
    echo ""
    echo -e "${RED}Failed tests:${NC}"
    for name in "${FAILED_TESTS[@]}"; do
        echo -e "  ${RED}✗${NC} $name"
    done
    exit 1
fi
