---
id: add-verify-stage-d34c
stage: done
risk: normal
deps: []
links: []
created: 2026-03-14T21:15:47Z
type: bug
priority: 1
tags: [pipeline]
---
# Add verify stage to all pipeline variants

Low-risk pipelines skip verify, so the orchestrator pushes code but never creates a PR. Code is unreachable.

## Fix

Add verify to every pipeline variant in pipelines.json. Every type x every risk level should include verify before done.

Current missing variants: any pipeline that goes directly from implement (or test) to done without verify.

## Test Results

**Timestamp:** 2026-03-14T21:30:00Z

**Test suite:** 114 tests across 3 packages, 114 passed, 0 failed
**Static analysis:** go vet clean

**Acceptance Criteria:**
- [x] Every pipeline variant includes verify before done (all 20 non-epic variants confirmed)
- [x] bug/low, task/low, and all chore/* variants fixed (all 7 variants now include verify)
- [x] Dead implement>done gate removed (no implement>done entry in gates section)

**Key tests:** TestPipelineLengths PASS, TestCheckGates_ChoreVerifyToDone PASS, TestPipelineFor_AllTypes PASS

No failures.

## Related

Forge verify skill will scale behavior based on risk level (separate ticket: add-verify-stage-8f22).

## Review Log

**2026-03-14T21:25:38Z [agent:impl-reviewer]**
APPROVED — All 3 acceptance criteria met. Bug/low, task/low, and all chore variants now include verify before done. Dead gate removed. Tests updated. No scope creep.

**2026-03-14T21:25:39Z [agent:code-reviewer]**
APPROVED — Clean, minimal change following existing conventions. No security concerns. Tests properly validate both failure and success paths for new transitions.

**2026-03-14T21:34:02Z [human:steve]**
APPROVED — Approved for release. Committed as v4.1.1.

## Notes

**2026-03-14T21:23:35Z**

Fix applied: added verify before done in bug/low, task/low, and all 5 chore variants. Removed dead implement>done gate. Updated 3 tests to match new pipeline shapes. All tests pass.

**2026-03-14T21:26:40Z**

## Test Results

**Timestamp:** 2026-03-14T21:30:00Z

### Automated
- **Type check:** N/A (Go, compiled language -- go vet clean)
- **Lint:** PASS (go vet ./... -- no issues)
- **Test suite:** 114 tests across 3 packages, 114 passed, 0 failed

### Acceptance Criteria
- [x] Every pipeline variant includes verify before done -- verified by reading pipelines.json: all 20 non-epic variants confirmed
- [x] bug/low, task/low, and all chore/* variants fixed -- verified by reading pipelines.json: all 7 variants now include verify
- [x] Dead implement>done gate removed -- verified by reading pipelines.json gates section: no implement>done entry exists

### Specific Test Coverage
- TestPipelineLengths: validates stage counts for all default pipelines (feature=8, bug=6, chore=5, epic=5, task=6) -- PASS
- TestCheckGates_ChoreVerifyToDone: validates verify->done gate for chores (fails without review, passes with review) -- PASS
- TestPipelineFor_AllTypes: validates all pipeline types resolve and start at backlog/end at done -- PASS

### Failures
None.

**2026-03-15T00:52:51Z**

Already implemented as part of add-backlog-stage-d553. All non-epic pipeline variants already include verify in pipelines.json.
