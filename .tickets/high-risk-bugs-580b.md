---
id: high-risk-bugs-580b
stage: done
risk: low
deps: []
links: []
created: 2026-03-15T06:12:34Z
type: feature
priority: 1
---
# High risk bugs should use full pipeline

Restructure pipelines.json so that:\n\n1. **default = normal** for all ticket types\n2. **bug high/critical** gets full pipeline (spec, design, design-review, impl, code-review, test, verify)\n3. **task** simplified to backlog → triage → done (research/documentation, not code)\n4. **chore** follows feature risk pipelines (was previously flat)\n5. **epic** unchanged\n\nThis is a config-only change to pipelines.json. Tests will need updating to match new pipeline shapes.

## Acceptance Criteria

1. When `PipelineFor(TypeFeature)` is called with no risk, the system shall return the normal pipeline `[backlog, triage, spec, design, design-review, implement, code-review, test, verify, done]` (10 stages), verified by `go test ./pkg/ticket/ -run TestPipelineLengths`.\n\n2. When `PipelineFor(TypeFeature, \"low\")` is called, the system shall return `[backlog, triage, spec, design, implement, test, verify, done]` (8 stages, no review stages), verified by `go test ./pkg/ticket/ -run TestPipelineFor_FeatureLow`.\n\n3. When `PipelineFor(TypeBug)` is called with no risk, the system shall return the normal pipeline `[backlog, triage, implement, code-review, test, verify, done]` (7 stages), verified by `go test ./pkg/ticket/ -run TestPipelineLengths`.\n\n4. When `PipelineFor(TypeBug, \"low\")` is called, the system shall return `[backlog, triage, implement, verify, done]` (5 stages), verified by `go test ./pkg/ticket/ -run TestPipelineFor_BugLow`.\n\n5. When `PipelineFor(TypeBug, \"high\")` is called, the system shall return the full pipeline `[backlog, triage, spec, design, design-review, implement, code-review, test, verify, done]` (10 stages), verified by `go test ./pkg/ticket/ -run TestPipelineFor_BugHighFull`.\n\n6. When `PipelineFor(TypeBug, \"critical\")` is called, the system shall return the same full pipeline as high (10 stages), verified by `go test ./pkg/ticket/ -run TestPipelineFor_BugCriticalEqHigh`.\n\n7. When `PipelineFor(TypeTask)` is called with any risk level (or no risk), the system shall return `[backlog, triage, done]` (3 stages), verified by `go test ./pkg/ticket/ -run TestPipelineLengths`.\n\n8. When `PipelineFor(TypeChore)` is called with no risk, the system shall return the same normal pipeline as feature `[backlog, triage, spec, design, design-review, implement, code-review, test, verify, done]` (10 stages), verified by `go test ./pkg/ticket/ -run TestPipelineLengths`.\n\n9. When `PipelineFor(TypeChore, \"low\")` is called, the system shall return the feature-low pipeline `[backlog, triage, spec, design, implement, test, verify, done]` (8 stages), verified by `go test ./pkg/ticket/ -run TestPipelineFor_ChoreLow`.\n\n10. When `PipelineFor(TypeEpic)` is called with any risk level, the system shall return `[backlog, triage, spec, design, done]` (5 stages, unchanged), verified by `go test ./pkg/ticket/ -run TestPipelineLengths`.\n\n11. When a task ticket advances from triage, the system shall transition to done, verified by `go test ./pkg/ticket/ -run TestNextStage`.\n\n12. When `HasStage(TypeChore, StageSpec)` is called, the system shall return true, verified by `go test ./pkg/ticket/ -run TestHasStage`.\n\n13. When `HasStage(TypeBug, StageSpec)` is called, the system shall return false (bug default/normal still lacks spec), verified by `go test ./pkg/ticket/ -run TestHasStage`.\n\n14. When `HasStageInPipeline(TypeBug, \"high\", StageSpec)` is called, the system shall return true, verified by `go test ./pkg/ticket/ -run TestHasStageInPipeline_BugHigh`.\n\n15. When a task ticket at triage advances, the `triage>done` gate shall require `description_exists`, verified by `go test ./pkg/ticket/ -run TestAdvance`.\n\n16. When all tests are run, the system shall pass with zero failures, verified by `go test ./...`.

## Design

## Design\n\nConfig-only change. `lookupPipeline()` already supports risk-based variant lookup with fallback to \"default\". No Go logic changes needed.\n\n### Step 1: Update `pkg/ticket/pipelines.json`\n\nReplace pipeline definitions with target matrix:\n- feature: default=normal (10 stages with design-review+code-review), low (8, no reviews)\n- bug: default=normal (7, triage→impl→code-review→test→verify), low (5), high/critical (10, full pipeline)\n- task: all variants [backlog, triage, done] (3 stages)\n- chore: mirrors feature pipelines exactly\n- epic: unchanged (5 stages)\n\nAdd gate: `\"triage>done\": {\"structural\": [\"description_exists\"]}`\n\n### Step 2: Update `pkg/ticket/pipeline_test.go`\n\n- `TestPipelineLengths`: feature 8→10, bug 6→7, chore 5→10, task 6→3\n- `TestHasStage`: chore now HAS spec/design/test (flip assertions). Bug spec/design stays false.\n- `TestNextStage`: Add task triage→done case\n- `TestStageIndex`: feature done 7→9, chore design -1→3\n- Add new tests for risk variants (bug high, chore low, etc.)\n- Add `TestHasStageInPipeline_BugHigh`\n\n### Step 3: Update `pkg/ticket/workflow_test.go`\n\n- `TestAdvance_NextStage`: chore triage→implement becomes triage→spec\n- `TestAdvance_ResetsReview`: same chore transition change\n- `TestSkip`: feature triage→implement skips 3 stages (was 2) due to design-review\n\n### Step 4: Update `pkg/ticket/filter_test.go`\n\n- `TestSortByStagePriorityID`: chore StageIndex(implement) changes 2→5, so t-005 sorts after t-004 (epic done=4). Update expected order to [t-003, t-001, t-002, t-004, t-005].\n\n### Step 5: Update `test-suite.sh`\n\n- Line ~627: chore advance assertion changes from `stage: implement` to `stage: spec`\n- Verify skip test (~line 666) still passes with more skipped stages\n\n### Step 6: Update `README.md`\n\n- Pipeline table (~lines 138-142): update task and chore pipeline descriptions\n- Gate scaling description (~line 144): risk now controls pipeline shape\n\n### Step 7: Update `CHANGELOG.md`\n\n- Add entry under `## [Unreleased]` / Changed\n\n### Files Affected\n1. `pkg/ticket/pipelines.json` — pipeline definitions + new gate\n2. `pkg/ticket/pipeline_test.go` — shape assertions + new risk variant tests\n3. `pkg/ticket/workflow_test.go` — advance/skip expectations\n4. `pkg/ticket/filter_test.go` — sort order assertion\n5. `test-suite.sh` — chore advance assertion\n6. `README.md` — pipeline documentation\n7. `CHANGELOG.md` — change log entry

## Test Results

56 tests, 56 passed, 0 failed (go test ./... across 3 packages). All 16 acceptance criteria verified individually.

## Review Log

**2026-03-15T07:07:53Z [human:steve]**
APPROVED — Spec approved. All 16 AC confirmed.

**2026-03-15T07:13:19Z [agent:design-reviewer]**
REJECTED — NEEDS REVISION: Design misses 3 files that will break: test-suite.sh (chore advance assertion), filter_test.go (sort order assertion), README.md (pipeline table). Fixes are mechanical additions.

**2026-03-15T07:15:39Z [human:steve]**
APPROVED — Design approved with reviewer findings incorporated.

**2026-03-15T07:20:40Z [agent:impl-reviewer]**
APPROVED — All 16 AC pass. Design followed across all 7 steps. No scope creep, no TODOs, no deviations.

**2026-03-15T07:20:45Z [agent:code-reviewer]**
APPROVED — No critical findings. Clean config change with well-structured tests. Style note: redundant risk variants for task/epic have documentation value.

**2026-03-15T07:35:35Z [human:steve]**
APPROVED — Verified. Ready to ship.

## Notes

**2026-03-15T06:45:47Z**

## Triage

**Risk:** low — Config-only change to pipelines.json plus test updates. No runtime logic changes.

**Scope:** Single task

**Key decisions:**
- default = normal across all types (human)
- bug high/critical gets spec + design + design-review (human)
- task reduced to backlog → triage → done, research only (human)
- chore promoted to follow feature pipelines (human)
- epic unchanged (human)

**2026-03-15T07:07:37Z**

## Spec\n\n**Scope:**\n- In: pipelines.json (pipeline definitions + triage>done gate), pipeline_test.go, workflow_test.go\n- Out: No Go logic changes. TUI/MCP/PipelineDescription read from config automatically. No ticket migration needed.\n\n**Decisions:**\n- 16 EARS-format acceptance criteria covering all type×risk combinations (auto)\n- New triage>done gate with description_exists for task pipeline (auto)\n- All decisions from triage carried forward (human)

**2026-03-15T07:20:52Z**

## Implement\n\n**Changes:**\n- pkg/ticket/pipelines.json: Restructured all pipeline definitions, added triage>done gate\n- pkg/ticket/pipeline_test.go: Updated length/stage assertions, added 7 new risk variant tests\n- pkg/ticket/workflow_test.go: Chore advance triage→spec, skip count 2→3\n- pkg/ticket/filter_test.go: Sort order updated for new chore StageIndex\n- test-suite.sh: Chore advance assertion updated\n- README.md: Pipeline table and gate description updated\n- CHANGELOG.md: 4 entries under Changed\n\n**Reviews:**\n- impl-reviewer: approved — all 16 AC pass, design followed\n- code-reviewer: approved — no critical findings
