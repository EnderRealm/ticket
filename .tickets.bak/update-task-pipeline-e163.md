---
id: update-task-pipeline-e163
stage: done
risk: low
deps: []
links: []
created: 2026-03-16T07:27:19Z
type: feature
priority: 2
tags: [pipeline, types]
skipped: [implement, test, verify]
---
# Update task pipeline to spec → design → verify (no implement)

Task type currently has a minimal pipeline: backlog → triage → done. Tasks are meant for thinking, design, and research — no coding. Update to: backlog → triage → spec → design → verify → done.

## Changes required

1. **pkg/ticket/pipelines.json** — Update task type stages to [backlog, triage, spec, design, verify, done] for all risk levels
2. **Gate checks** — Review whether design→verify transition needs a gate entry (existing gates cover design→implement but not design→verify)
3. **Tests** — Update any tests validating task pipeline stage sequences

## Context

Parent work in forge repo: differentiate-task-type-f1a6. The forge orchestrator must match, but tk is the enforcer.

## Acceptance Criteria

1. When `PipelineFor(TypeTask)` is called with no risk argument, the system shall return the stage sequence `[backlog, triage, spec, design, verify, done]`, verified by `go test ./pkg/ticket/ -run TestPipelineLengths`.

2. When `PipelineFor(TypeTask, risk)` is called for any risk level (low, normal, high, critical), the system shall return the same 6-stage sequence `[backlog, triage, spec, design, verify, done]`, verified by `go test ./pkg/ticket/ -run TestPipelineFor_TaskAllRisks`.

3. When `HasStage(TypeTask, StageSpec)` is called, the system shall return `true`, verified by `go test ./pkg/ticket/ -run TestHasStage`.

4. When `HasStage(TypeTask, StageDesign)` is called, the system shall return `true`, verified by `go test ./pkg/ticket/ -run TestHasStage`.

5. When `HasStage(TypeTask, StageVerify)` is called, the system shall return `true`, verified by `go test ./pkg/ticket/ -run TestHasStage`.

6. When `HasStage(TypeTask, StageImplement)` is called, the system shall return `false`, verified by `go test ./pkg/ticket/ -run TestHasStage`.

7. When `HasStage(TypeTask, StageDesignReview)` is called, the system shall return `false`, verified by `go test ./pkg/ticket/ -run TestHasStage`.

8. When `NextStage(TypeTask, StageTriage)` is called, the system shall return `(StageSpec, true)`, verified by `go test ./pkg/ticket/ -run TestNextStage`.

9. When `NextStage(TypeTask, StageDesign)` is called, the system shall return `(StageVerify, true)`, verified by `go test ./pkg/ticket/ -run TestNextStage`.

10. When a task ticket at stage `design` attempts to advance to `verify`, the system shall enforce the `design>verify` gate requiring `design_exists`, verified by `go test ./pkg/ticket/ -run TestCheckGates_DesignToVerify`.

11. When a task ticket at stage `design` attempts to advance to `verify` without a design section, the system shall reject the transition with a gate failure, verified by `go test ./pkg/ticket/ -run TestCheckGates_DesignToVerify`.

12. When a task ticket at stage `design` attempts to advance to `verify`, the system shall NOT require `review_approved`, verified by `go test ./pkg/ticket/ -run TestCheckGates_DesignToVerify`.

13. When `PipelineDescription()` is called, the system shall include the task pipeline showing `backlog -> triage -> spec -> design -> verify -> done`, verified by `go test ./pkg/ticket/ -run TestPipelineDescription` or manual check with `tk pipelines`.

## Design

## Design\n\nCancelled — product decision: tasks should remain backlog → triage → done. No changes needed.

## Review Log

**2026-03-16T07:42:03Z [agent:spec-builder]**
APPROVED — All 13 criteria follow EARS format, are testable with clear pass/fail, and correctly scoped. One minor note: criterion 13 uses ASCII `->` but PipelineDescription() outputs Unicode `→` — implementer should match actual output. Optional gap: no explicit NextStage criteria for spec->design and verify->done transitions, but these are implicitly covered by pipeline sequence assertions in AC 1-2.

**2026-03-16T07:44:37Z [agent:design-reviewer]**
APPROVED — All file paths, APIs, types, and patterns verified. Gate key format matches convention. design_exists check exists. All 13 AC covered. Warnings: (1) spec>design gate requires acceptance_exists + review_approved — heavier than old triage>done for tasks; (2) verify>done requires review_approved — also heavier; (3) no explicit PipelineDescription test. These are product decisions, not implementation errors.

## Notes

**2026-03-16T07:36:08Z**

## Triage

**Risk:** low — Isolated config change to pipelines.json, one gate addition, and test updates. No code logic changes, no external dependencies, no auth/payment/PII impact.

**Priority:** 2 — Standard priority. Part of parent work (differentiate-task-type-f1a6 in forge) but not blocking production.

**Scope:** Single task. Three changes: pipelines.json task entries, new `design>verify` gate (`design_exists` only), test updates.

**Key decisions:**
- `design>verify` gate requires only `design_exists`, not `review_approved` (human)
- Task pipeline skips `design-review` intentionally — tasks are thinking/research, not code (human)

**2026-03-16T07:39:03Z**

## Spec

**Scope:** pipelines.json (task stages + design>verify gate), pipeline_test.go (update existing tests), gates_test.go (new gate test). Out of scope: MCP tests, TUI, forge orchestrator, existing spec>design gate.

**Decisions:**
- 13 acceptance criteria covering pipeline stages, HasStage, NextStage, gate enforcement, and pipeline description (human)
- Existing spec>design gate applies automatically to tasks, no change needed (auto)
