#!/usr/bin/env bash
# Comprehensive test suite for tk ticket system
# Creates test tickets, exercises all features, then cleans up

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

PASS=0
FAIL=0
TEST_IDS=()

log_pass() {
    echo -e "${GREEN}✓${NC} $1"
    PASS=$((PASS + 1))
}

log_fail() {
    echo -e "${RED}✗${NC} $1"
    FAIL=$((FAIL + 1))
}

log_section() {
    echo -e "\n${YELLOW}=== $1 ===${NC}"
}

# Track test ticket IDs for cleanup
track_id() {
    TEST_IDS+=("$1")
}

# Extract ID from tk create output
extract_id() {
    grep "^id:" | awk '{print $2}'
}

# Assert command succeeds
assert_ok() {
    if eval "$1" > /dev/null 2>&1; then
        log_pass "$2"
    else
        log_fail "$2"
    fi
}

# Assert command fails
assert_fail() {
    if eval "$1" > /dev/null 2>&1; then
        log_fail "$2 (should have failed)"
    else
        log_pass "$2"
    fi
}

# Assert output contains string
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

# Assert output does NOT contain string
assert_not_contains() {
    local output
    output=$(eval "$1" 2>&1) || true
    if echo "$output" | grep -q "$2"; then
        log_fail "$3 (unexpected '$2' in output)"
    else
        log_pass "$3"
    fi
}

cleanup() {
    log_section "CLEANUP"
    for id in "${TEST_IDS[@]}"; do
        tk delete "$id" 2>/dev/null || true
    done
    echo "Deleted ${#TEST_IDS[@]} test tickets"
}

trap cleanup EXIT

# ============================================================================
log_section "TICKET CREATION"
# ============================================================================

# Basic create
ID1=$(tk create "Test Basic Ticket" | extract_id)
track_id "$ID1"
if [[ -n "$ID1" ]]; then
    log_pass "Create basic ticket: $ID1"
else
    log_fail "Create basic ticket"
fi

# Create with description
ID2=$(tk create "Test With Description" -d "This is a description" | extract_id)
track_id "$ID2"
assert_contains "tk show $ID2" "This is a description" "Create with description"

# Create with all options
ID3=$(tk create "Test Full Options" \
    -d "Full description" \
    --design "Design notes here" \
    --acceptance "AC: it works" \
    -t epic \
    -p 1 \
    -a "Tester" \
    --external-ref "GH-123" \
    --tags "test,automated" | extract_id)
track_id "$ID3"
assert_contains "tk show $ID3" "type: epic" "Create with type"
assert_contains "tk show $ID3" "priority: 1" "Create with priority"
assert_contains "tk show $ID3" "assignee: Tester" "Create with assignee"
assert_contains "tk show $ID3" "external-ref: GH-123" "Create with external-ref"
assert_contains "tk show $ID3" "tags:" "Create with tags"
assert_contains "tk show $ID3" "## Design" "Create with design section"
assert_contains "tk show $ID3" "## Acceptance Criteria" "Create with acceptance section"

# Create different types
for type in task feature bug chore; do
    ID=$(tk create "Test $type type" -t "$type" | extract_id)
    track_id "$ID"
    assert_contains "tk show $ID" "type: $type" "Create type: $type"
done

# Create with parent
ID_CHILD=$(tk create "Test Child Ticket" --parent "$ID3" | extract_id)
track_id "$ID_CHILD"
assert_contains "tk show $ID_CHILD" "parent: $ID3" "Create with parent"
assert_contains "tk show $ID3" "$ID_CHILD" "Parent shows child"

# ============================================================================
log_section "TICKET EDITING"
# ============================================================================

# Edit title
assert_ok "tk edit $ID1 --title 'Updated Title'" "Edit title"
assert_contains "tk show $ID1" "# Updated Title" "Title was updated"

# Edit description
assert_ok "tk edit $ID1 -d 'New description text'" "Edit description"
assert_contains "tk show $ID1" "New description text" "Description was updated"

# Edit design
assert_ok "tk edit $ID1 --design 'New design notes'" "Edit design"
assert_contains "tk show $ID1" "## Design" "Design section added"

# Edit acceptance
assert_ok "tk edit $ID1 --acceptance 'New AC'" "Edit acceptance"
assert_contains "tk show $ID1" "## Acceptance Criteria" "AC section added"

# Edit status
assert_ok "tk edit $ID1 -s in_progress" "Edit status to in_progress"
assert_contains "tk show $ID1" "status: in_progress" "Status is in_progress"

assert_ok "tk edit $ID1 -s needs_testing" "Edit status to needs_testing"
assert_contains "tk show $ID1" "status: needs_testing" "Status is needs_testing"

assert_ok "tk edit $ID1 -s closed" "Edit status to closed"
assert_contains "tk show $ID1" "status: closed" "Status is closed"

assert_ok "tk edit $ID1 -s open" "Edit status to open (reopen)"
assert_contains "tk show $ID1" "status: open" "Status is open"

# Edit type
assert_ok "tk edit $ID1 -t bug" "Edit type"
assert_contains "tk show $ID1" "type: bug" "Type was updated"

# Edit priority
assert_ok "tk edit $ID1 -p 0" "Edit priority"
assert_contains "tk show $ID1" "priority: 0" "Priority was updated"

# Edit assignee
assert_ok "tk edit $ID1 -a 'New Assignee'" "Edit assignee"
assert_contains "tk show $ID1" "assignee: New Assignee" "Assignee was updated"

# Edit tags
assert_ok "tk edit $ID1 --tags 'new,tags,here'" "Edit tags"
assert_contains "tk show $ID1" "tags:" "Tags were updated"

# Invalid status should fail
assert_fail "tk edit $ID1 -s invalid_status" "Reject invalid status"

# Invalid priority should fail
assert_fail "tk edit $ID1 -p 99" "Reject invalid priority"

# ============================================================================
log_section "DEPENDENCIES"
# ============================================================================

# Create tickets for dependency testing
DEP1=$(tk create "Dep Test 1" | extract_id)
track_id "$DEP1"
DEP2=$(tk create "Dep Test 2" | extract_id)
track_id "$DEP2"
DEP3=$(tk create "Dep Test 3" | extract_id)
track_id "$DEP3"

# Add dependency
assert_ok "tk dep $DEP2 $DEP1" "Add dependency"
assert_contains "tk show $DEP2" "deps: \[$DEP1\]" "Dependency recorded"

# Ticket with unresolved dep should be blocked
assert_contains "tk blocked" "$DEP2" "Blocked ticket appears in blocked list"
assert_not_contains "tk ready" "$DEP2" "Blocked ticket not in ready list"

# Close dependency
tk edit "$DEP1" -s closed
assert_contains "tk ready --open" "$DEP2" "Ticket ready after dep closed"

# Remove dependency
tk edit "$DEP1" -s open
assert_ok "tk undep $DEP2 $DEP1" "Remove dependency"
assert_contains "tk show $DEP2" "deps: \[\]" "Dependency removed"

# Dependency tree
tk dep "$DEP2" "$DEP1"
tk dep "$DEP3" "$DEP2"
assert_contains "tk dep tree $DEP3" "$DEP2" "Dep tree shows dependencies"
assert_contains "tk dep tree $DEP3" "$DEP1" "Dep tree shows transitive deps"

# Cycle detection - create a cycle
tk dep "$DEP1" "$DEP3"
assert_contains "tk dep cycle" "Cycle" "Cycle detection finds cycles"
tk undep "$DEP1" "$DEP3"  # Remove cycle

# ============================================================================
log_section "LINKS"
# ============================================================================

LINK1=$(tk create "Link Test 1" | extract_id)
track_id "$LINK1"
LINK2=$(tk create "Link Test 2" | extract_id)
track_id "$LINK2"

# Add link
assert_ok "tk link $LINK1 $LINK2" "Add link"
assert_contains "tk show $LINK1" "$LINK2" "Link appears in first ticket"
assert_contains "tk show $LINK2" "$LINK1" "Link is symmetric"

# Remove link
assert_ok "tk unlink $LINK1 $LINK2" "Remove link"
assert_not_contains "tk show $LINK1" "links: \[$LINK2\]" "Link removed from first"

# ============================================================================
log_section "LISTING AND FILTERING"
# ============================================================================

# Create tickets for filter testing
FILTER1=$(tk create "Filter Test Alpha" -t task -p 1 -a "Alice" --tags "frontend" | extract_id)
track_id "$FILTER1"
FILTER2=$(tk create "Filter Test Beta" -t bug -p 2 -a "Bob" --tags "backend" | extract_id)
track_id "$FILTER2"
tk edit "$FILTER2" -s closed

# Basic ls
assert_contains "tk ls" "$FILTER1" "ls shows open tickets"

# Filter by status
assert_contains "tk ls --status=open" "$FILTER1" "Filter by status=open"
assert_not_contains "tk ls --status=open" "$FILTER2" "Filter excludes closed"
assert_contains "tk ls --status=closed" "$FILTER2" "Filter by status=closed"

# Filter by assignee
assert_contains "tk ls -a Alice" "$FILTER1" "Filter by assignee"
assert_not_contains "tk ls -a Alice" "$FILTER2" "Assignee filter excludes others"

# Filter by tag
assert_contains "tk ls -T frontend" "$FILTER1" "Filter by tag"
assert_not_contains "tk ls -T frontend" "$FILTER2" "Tag filter excludes others"

# Filter by priority
assert_contains "tk ls -P 1" "$FILTER1" "Filter by priority"
assert_not_contains "tk ls -P 1" "$FILTER2" "Priority filter excludes others"

# Filter by type
assert_contains "tk ls -t task" "$FILTER1" "Filter by type"
assert_not_contains "tk ls -t task" "$FILTER2" "Type filter excludes others"

# Closed list
assert_contains "tk closed" "$FILTER2" "Closed list shows closed tickets"

# ============================================================================
log_section "HIERARCHY GATING"
# ============================================================================

# Create epic and children
EPIC=$(tk create "Test Epic for Gating" -t epic | extract_id)
track_id "$EPIC"
CHILD1=$(tk create "Epic Child 1" --parent "$EPIC" | extract_id)
track_id "$CHILD1"
CHILD2=$(tk create "Epic Child 2" --parent "$EPIC" | extract_id)
track_id "$CHILD2"

# Children should NOT appear in ready when epic is open
assert_not_contains "tk ready" "$CHILD1" "Child hidden when epic is open"

# Children SHOULD appear with --open flag
assert_contains "tk ready --open" "$CHILD1" "Child visible with --open"

# Start epic - children should now be ready
tk edit "$EPIC" -s in_progress
assert_contains "tk ready" "$CHILD1" "Child visible when epic in_progress"
assert_contains "tk ready" "$CHILD2" "Both children visible"

# ============================================================================
log_section "STATUS PROPAGATION"
# ============================================================================

# Use the epic and children from hierarchy gating
# Reset states
tk edit "$EPIC" -s in_progress
tk edit "$CHILD1" -s open
tk edit "$CHILD2" -s open

# Close first child - epic should stay in_progress
tk edit "$CHILD1" -s closed
assert_contains "tk show $EPIC" "status: in_progress" "Epic stays in_progress with open child"

# Set second child to needs_testing - epic should become needs_testing
tk edit "$CHILD2" -s needs_testing
assert_contains "tk show $EPIC" "status: needs_testing" "Epic propagates to needs_testing"

# Close second child - epic should become closed
tk edit "$CHILD2" -s closed
assert_contains "tk show $EPIC" "status: closed" "Epic propagates to closed"

# ============================================================================
log_section "NOTES"
# ============================================================================

NOTE_ID=$(tk create "Note Test Ticket" | extract_id)
track_id "$NOTE_ID"

# Add note
assert_ok "tk add-note $NOTE_ID 'This is a test note'" "Add note"
assert_contains "tk show $NOTE_ID" "This is a test note" "Note appears in ticket"
assert_contains "tk show $NOTE_ID" "## Notes" "Notes section created"

# Add another note
assert_ok "tk add-note $NOTE_ID 'Second note'" "Add second note"
assert_contains "tk show $NOTE_ID" "Second note" "Second note appears"

# ============================================================================
log_section "QUERY (JSON)"
# ============================================================================

# Basic query
assert_ok "tk query" "Query outputs JSON"
assert_contains "tk query" '"id":' "Query contains id field"
assert_contains "tk query" '"status":' "Query contains status field"

# Query with filter (if jq available)
if command -v jq &> /dev/null; then
    assert_contains "tk query 'select(.type==\"epic\")'" "epic" "Query with jq filter"
fi

# ============================================================================
log_section "WORKFLOW COMMAND"
# ============================================================================

assert_contains "tk workflow" "Ticket Workflow Guide" "Workflow outputs guide"
assert_contains "tk workflow" "Ticket Types" "Workflow has types section"
assert_contains "tk workflow" "Statuses" "Workflow has statuses section"
assert_contains "tk workflow" "Readiness Rules" "Workflow has readiness section"
assert_contains "tk workflow" "Status Propagation" "Workflow has propagation section"

# ============================================================================
log_section "HELP"
# ============================================================================

assert_contains "tk help" "Usage:" "Help shows usage"
assert_contains "tk help" "create" "Help shows create command"
assert_contains "tk help" "edit" "Help shows edit command"
assert_contains "tk help" "\-s, \-\-status" "Help shows status flag"
assert_contains "tk --help" "Usage:" "--help flag works"
assert_contains "tk -h" "Usage:" "-h flag works"

# ============================================================================
log_section "PARTIAL ID MATCHING"
# ============================================================================

# Use first 4 chars of an existing ticket ID
PARTIAL=${FILTER1:0:4}
assert_contains "tk show $PARTIAL" "$FILTER1" "Partial ID resolves to full ticket"

# ============================================================================
log_section "DEP TREE --full"
# ============================================================================

# DEP1 <- DEP2 <- DEP3 chain still exists from DEPENDENCIES section
# --full should repeat shared nodes instead of deduplicating
assert_ok "tk dep tree --full $DEP3" "dep tree --full succeeds"
assert_contains "tk dep tree $DEP3" "$DEP1" "dep tree shows transitive dep"
assert_contains "tk dep tree --full $DEP3" "$DEP1" "dep tree --full shows transitive dep"

# ============================================================================
log_section "CLOSED --limit"
# ============================================================================

# Create several closed tickets
CL1=$(tk create "Closed Limit A" | extract_id); track_id "$CL1"
CL2=$(tk create "Closed Limit B" | extract_id); track_id "$CL2"
CL3=$(tk create "Closed Limit C" | extract_id); track_id "$CL3"
tk edit "$CL1" -s closed
tk edit "$CL2" -s closed
tk edit "$CL3" -s closed

# --limit=1 should only show 1 ticket
CL_OUTPUT=$(tk closed --limit=1 2>&1)
CL_COUNT=$(echo "$CL_OUTPUT" | grep -c "^[a-z]" || true)
if [[ "$CL_COUNT" -le 2 ]]; then
    log_pass "closed --limit=1 constrains output"
else
    log_fail "closed --limit=1 constrains output (got $CL_COUNT lines)"
fi

# ============================================================================
log_section "ADD-NOTE VIA STDIN"
# ============================================================================

STDIN_ID=$(tk create "Stdin Note Test" | extract_id)
track_id "$STDIN_ID"
echo "Note from stdin" | tk add-note "$STDIN_ID"
assert_contains "tk show $STDIN_ID" "Note from stdin" "add-note reads from stdin"

# ============================================================================
log_section "LS --parent FILTER"
# ============================================================================

# EPIC/CHILD1/CHILD2 are closed from propagation tests — use --status=closed
assert_contains "tk ls --parent=$EPIC --status=closed" "$CHILD1" "ls --parent shows child"
assert_not_contains "tk ls --parent=$EPIC --status=closed" "$FILTER1" "ls --parent excludes non-children"

# ============================================================================
log_section "READY/BLOCKED/CLOSED FILTERS"
# ============================================================================

# Create tickets with distinct assignees and tags for filter testing
RF1=$(tk create "Ready Filter 1" -a "FilterAlice" --tags "readytest" | extract_id)
track_id "$RF1"
RF2=$(tk create "Ready Filter 2" -a "FilterBob" --tags "othertest" | extract_id)
track_id "$RF2"

# ready -a filter
RF_READY=$(tk ready -a FilterAlice 2>&1) || true
if echo "$RF_READY" | grep -q "$RF1"; then
    log_pass "ready -a filters by assignee (includes match)"
else
    # RF1 might not be ready due to parent gating - check with --open
    RF_READY_OPEN=$(tk ready --open -a FilterAlice 2>&1) || true
    if echo "$RF_READY_OPEN" | grep -q "$RF1"; then
        log_pass "ready --open -a filters by assignee (includes match)"
    else
        log_fail "ready -a filters by assignee"
    fi
fi

# ready -T filter
RF_TAG=$(tk ready -T readytest 2>&1) || true
RF_TAG_OPEN=$(tk ready --open -T readytest 2>&1) || true
if echo "$RF_TAG" | grep -q "$RF1" || echo "$RF_TAG_OPEN" | grep -q "$RF1"; then
    log_pass "ready -T filters by tag (includes match)"
else
    log_fail "ready -T filters by tag"
fi

# blocked -a filter: make RF1 blocked
BF_DEP=$(tk create "Blocker for filter" | extract_id)
track_id "$BF_DEP"
tk dep "$RF1" "$BF_DEP"
assert_contains "tk blocked -a FilterAlice" "$RF1" "blocked -a filters by assignee"
assert_not_contains "tk blocked -a FilterAlice" "$RF2" "blocked -a excludes non-match"

# blocked -T filter
assert_contains "tk blocked -T readytest" "$RF1" "blocked -T filters by tag"
assert_not_contains "tk blocked -T readytest" "$RF2" "blocked -T excludes non-match"

# closed -a filter
tk undep "$RF1" "$BF_DEP"
tk edit "$RF1" -s closed
assert_contains "tk closed -a FilterAlice" "$RF1" "closed -a filters by assignee"
assert_not_contains "tk closed -a FilterAlice" "$RF2" "closed -a excludes non-match"

# closed -T filter
assert_contains "tk closed -T readytest" "$RF1" "closed -T filters by tag"
assert_not_contains "tk closed -T readytest" "$RF2" "closed -T excludes non-match"

# ============================================================================
log_section "SHOW MULTIPLE IDS"
# ============================================================================

MULTI_OUTPUT=$(tk show "$FILTER1" "$NOTE_ID" 2>&1)
if echo "$MULTI_OUTPUT" | grep -q "$FILTER1" && echo "$MULTI_OUTPUT" | grep -q "$NOTE_ID"; then
    log_pass "show accepts multiple IDs"
else
    log_fail "show accepts multiple IDs"
fi

# ============================================================================
log_section "LS --group-by"
# ============================================================================

# group-by workflow
assert_contains "tk ls --group-by=workflow" "===" "group-by workflow shows group headers"

# group-by type
assert_contains "tk ls --group-by=type" "===" "group-by type shows group headers"
assert_contains "tk ls --group-by=type" "task" "group-by type includes task group"

# group-by status
assert_contains "tk ls --group-by=status" "===" "group-by status shows group headers"

# group-by priority
assert_contains "tk ls --group-by=priority" "===" "group-by priority shows group headers"

# --group shorthand
assert_contains "tk ls --group" "===" "--group shorthand works"

# invalid group-by
assert_fail "tk ls --group-by=invalid" "Reject invalid group-by value"

# ============================================================================
log_section "STATS"
# ============================================================================

assert_contains "tk stats" "PROJECT HEALTH" "stats shows header"
assert_contains "tk stats" "Status:" "stats shows status section"
assert_contains "tk stats" "Types:" "stats shows types section"
assert_contains "tk stats" "Priority:" "stats shows priority section"
assert_contains "tk stats" "TOTAL" "stats shows total count"

# ============================================================================
log_section "TIMELINE"
# ============================================================================

assert_contains "tk timeline" "TICKETS CLOSED BY WEEK" "timeline shows header"
assert_ok "tk timeline --weeks=2" "timeline --weeks flag works"

# ============================================================================
log_section "MOVE"
# ============================================================================

# Set up target repo
MOVE_TARGET=$(mktemp -d)
mkdir -p "$MOVE_TARGET/.tickets"

# Single move
MOVE1=$(tk create "Move Test Single" -d "Will be moved" --tags "movetest" | extract_id)
track_id "$MOVE1"
MOVE_OUTPUT=$(tk move "$MOVE1" "$MOVE_TARGET" 2>&1)
if echo "$MOVE_OUTPUT" | grep -q "Moved $MOVE1"; then
    log_pass "Single move succeeds"
else
    log_fail "Single move succeeds"
fi

# Source should be closed
assert_contains "tk show $MOVE1" "status: closed" "Source ticket closed after move"
assert_contains "tk show $MOVE1" "Moved to" "Source has move note"

# Target should exist
MOVE1_NEW=$(echo "$MOVE_OUTPUT" | sed -n 's/.*-> //p')
if TICKETS_DIR="$MOVE_TARGET/.tickets" tk show "$MOVE1_NEW" > /dev/null 2>&1; then
    log_pass "Target ticket exists after move"
else
    log_fail "Target ticket exists after move"
fi

# Recursive move
MOVE_EPIC=$(tk create "Move Epic" -t epic | extract_id)
track_id "$MOVE_EPIC"
MOVE_CH1=$(tk create "Move Child A" --parent "$MOVE_EPIC" | extract_id)
track_id "$MOVE_CH1"
MOVE_CH2=$(tk create "Move Child B" --parent "$MOVE_EPIC" | extract_id)
track_id "$MOVE_CH2"
tk dep "$MOVE_CH2" "$MOVE_CH1"

REC_OUTPUT=$(tk move "$MOVE_EPIC" "$MOVE_TARGET" -r 2>&1)
REC_COUNT=$(echo "$REC_OUTPUT" | grep -c "Moved")
if [[ "$REC_COUNT" -eq 3 ]]; then
    log_pass "Recursive move moves 3 tickets"
else
    log_fail "Recursive move moves 3 tickets (got $REC_COUNT)"
fi

# All sources should be closed
assert_contains "tk show $MOVE_EPIC" "status: closed" "Epic closed after recursive move"
assert_contains "tk show $MOVE_CH1" "status: closed" "Child closed after recursive move"

# Invalid target should fail
assert_fail "tk move $MOVE1 /tmp/nonexistent-repo-xyz" "Move to invalid target fails"

rm -rf "$MOVE_TARGET"

# ============================================================================
log_section "STAGE PIPELINE — CREATE WITH STAGE"
# ============================================================================

# New tickets should get stage: triage
STAGE1=$(tk create "Stage Test Feature" -t feature | extract_id)
track_id "$STAGE1"
assert_contains "tk show $STAGE1" "stage: triage" "New ticket has stage: triage"

STAGE2=$(tk create "Stage Test Bug" -t bug | extract_id)
track_id "$STAGE2"
assert_contains "tk show $STAGE2" "stage: triage" "New bug has stage: triage"

# ============================================================================
log_section "STAGE PIPELINE — ADVANCE"
# ============================================================================

# Advance a chore: triage → spec (chore now follows feature pipeline)
CHORE=$(tk create "Advance Test Chore" -t chore -d "Has a description" | extract_id)
track_id "$CHORE"
assert_ok "tk advance $CHORE" "Advance chore triage → spec"
assert_contains "tk show $CHORE" "stage: spec" "Chore advanced to spec"

# Advance without gate satisfaction should fail
FEAT=$(tk create "Advance Test Feature" -t feature -d "Feature description" | extract_id)
track_id "$FEAT"
assert_ok "tk advance $FEAT" "Advance feature triage → spec"
assert_contains "tk show $FEAT" "stage: spec" "Feature advanced to spec"

# spec → design requires AC + review approved — should fail without them
assert_fail "tk advance $FEAT" "Advance spec → design fails without AC and review"

# Force bypass
assert_ok "tk advance $FEAT --force" "Advance with --force bypasses gates"
assert_contains "tk show $FEAT" "stage: design" "Feature forced to design"

# ============================================================================
log_section "STAGE PIPELINE — REVIEW"
# ============================================================================

# Review a ticket
assert_ok "tk review $FEAT --approve --comment 'Design looks good'" "Approve review"
assert_contains "tk show $FEAT" "review: approved" "Review state is approved"
assert_contains "tk log $FEAT" "approved" "Log shows review verdict"

# Reject
assert_ok "tk review $FEAT --reject --comment 'Needs rework'" "Reject review"
assert_contains "tk show $FEAT" "review: rejected" "Review state is rejected"

# Must specify exactly one of approve/reject
assert_fail "tk review $FEAT" "Review without approve/reject fails"

# ============================================================================
log_section "STAGE PIPELINE — SKIP"
# ============================================================================

SKIP_FEAT=$(tk create "Skip Test Feature" -t feature -d "Skip test" | extract_id)
track_id "$SKIP_FEAT"

# Skip from triage to implement
assert_ok "tk skip $SKIP_FEAT --to implement --reason 'trivial feature'" "Skip to implement"
assert_contains "tk show $SKIP_FEAT" "stage: implement" "Skipped to implement"
assert_contains "tk show $SKIP_FEAT" "skipped:" "Skipped stages recorded"

# Skip backward should fail
assert_fail "tk skip $SKIP_FEAT --to triage --reason 'oops'" "Skip backward fails"

# Skip without reason should fail
SKIP2=$(tk create "Skip No Reason" -t feature -d "test" | extract_id)
track_id "$SKIP2"
assert_fail "tk skip $SKIP2 --to implement" "Skip without reason fails"

# ============================================================================
log_section "STAGE PIPELINE — LOG"
# ============================================================================

assert_contains "tk log $FEAT" "Current stage:" "Log shows current stage"
assert_contains "tk log $FEAT" "Review Log:" "Log shows review history"

# ============================================================================
log_section "STAGE PIPELINE — PIPELINE VIEW"
# ============================================================================

assert_ok "tk pipeline" "Pipeline command succeeds"
assert_contains "tk pipeline" "===" "Pipeline shows stage groups"

# Filter by stage
assert_ok "tk pipeline --stage=triage" "Pipeline --stage filter works"

# ============================================================================
log_section "STAGE PIPELINE — INBOX"
# ============================================================================

# Triage tickets need human attention
INBOX_TICKET=$(tk create "Inbox Test" -t feature -d "Needs triage" | extract_id)
track_id "$INBOX_TICKET"
assert_contains "tk inbox" "$INBOX_TICKET" "Inbox shows triage ticket"
assert_contains "tk inbox" "human-input" "Inbox shows action kind"

# ============================================================================
log_section "STAGE PIPELINE — NEXT"
# ============================================================================

# Create an epic with children to test projects view
NEXT_EPIC=$(tk create "Next Test Epic" -t epic | extract_id)
track_id "$NEXT_EPIC"
tk edit "$NEXT_EPIC" -s in_progress
NEXT_CH1=$(tk create "Next Child 1" --parent "$NEXT_EPIC" -d "child" | extract_id)
track_id "$NEXT_CH1"
assert_contains "tk next" "$NEXT_EPIC" "Next shows epic"
assert_contains "tk next" "triage" "Next shows stage breakdown"

# ============================================================================
log_section "STAGE PIPELINE — MIGRATE"
# ============================================================================

# Dry run should report what would change
assert_ok "tk migrate --dry-run" "Migrate dry run succeeds"

# ============================================================================
log_section "STAGE PIPELINE — BACKWARD COMPAT"
# ============================================================================

# close should work on stage-based tickets
COMPAT=$(tk create "Compat Test" -t chore -d "test" | extract_id)
track_id "$COMPAT"
assert_ok "tk close $COMPAT" "close works on stage-based ticket"
assert_contains "tk show $COMPAT" "done" "Closed ticket shows stage: done"

# reopen should work
assert_ok "tk reopen $COMPAT" "reopen works on stage-based ticket"
assert_contains "tk show $COMPAT" "triage" "Reopened ticket shows stage: triage"

# ============================================================================
log_section "STAGE PIPELINE — LS GROUP-BY PIPELINE"
# ============================================================================

assert_contains "tk ls --group-by=pipeline" "===" "group-by pipeline shows groups"

# ============================================================================
log_section "STAGE PIPELINE — EDIT STAGE/REVIEW/RISK"
# ============================================================================

EDIT_STAGE=$(tk create "Edit Stage Test" -t task -d "test" | extract_id)
track_id "$EDIT_STAGE"
assert_ok "tk edit $EDIT_STAGE --stage implement" "Edit stage via --stage"
assert_contains "tk show $EDIT_STAGE" "stage: implement" "Stage edited"

assert_ok "tk edit $EDIT_STAGE --review pending" "Edit review via --review"
assert_contains "tk show $EDIT_STAGE" "review: pending" "Review edited"

assert_ok "tk edit $EDIT_STAGE --risk high" "Edit risk via --risk"
assert_contains "tk show $EDIT_STAGE" "risk: high" "Risk edited"

# Invalid stage
assert_fail "tk edit $EDIT_STAGE --stage invalid" "Reject invalid stage"
# Invalid review
assert_fail "tk edit $EDIT_STAGE --review invalid" "Reject invalid review state"
# Invalid risk
assert_fail "tk edit $EDIT_STAGE --risk invalid" "Reject invalid risk level"

# ============================================================================
log_section "ERROR HANDLING"
# ============================================================================

# Unknown command
assert_fail "tk unknown_command" "Reject unknown command"

# Missing required args
assert_fail "tk edit" "Reject edit without id"
assert_fail "tk dep" "Reject dep without args"
assert_fail "tk delete" "Reject delete without id"

# Invalid ticket ID
assert_fail "tk show nonexistent_id_xyz" "Reject nonexistent ticket"

# ============================================================================
log_section "DELETE"
# ============================================================================

DELETE_ID=$(tk create "To Be Deleted" | extract_id)
assert_ok "tk delete $DELETE_ID" "Delete ticket"
assert_fail "tk show $DELETE_ID" "Deleted ticket not found"

# ============================================================================
# RESULTS
# ============================================================================

echo ""
echo "========================================"
echo -e "  ${GREEN}PASSED: $PASS${NC}"
echo -e "  ${RED}FAILED: $FAIL${NC}"
echo "========================================"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
