---
id: deprecate-migrate-beads-d975
stage: done
risk: low
deps: []
links: []
created: 2026-03-12T19:04:38Z
type: chore
priority: 4
run_id: orch-mmr3v0yu-p83i5r
---
# Deprecate 'migrate-beads'

Remove the migrate-beads reference from help text in cmd/root.go and README.md. The command was never implemented — ticket tic-303f explicitly decided not to port it ("Will not build. Migrate-beads is a legacy one-time migration tool, not worth porting.").

## Notes

**2026-03-15T01:56:07Z**

## Triage

**Risk:** low — docs-only change, removes references to a never-implemented command from help text and README

**Scope:** single task

**Key decisions:**
- Ticket already well-defined from backlog, no additional scoping needed (auto)

**2026-03-15T01:59:09Z**

### Agent: Grimnar (surgical-coder) during implement @ 2026-03-15T01:59:09.632Z
### #####################################################################

## Summary

**What changed and why:**
- Removed the `migrate-beads              Import from .beads/issues.jsonl` line from the help text in `cmd/root.go` (line 83). This command was never implemented — it was a legacy migration tool explicitly scoped out in ticket tic-303f.
- README.md had no `migrate-beads` reference, so no change was needed there.
- CHANGELOG.md has a historical reference (v-something changelog entry) which is appropriate to leave as-is — changelogs document history.

**Files modified:**
- `cmd/root.go` — removed line 83 (`migrate-beads` help text entry) from the `helpText` string

**Verification results:**
- Build: PASS (`go build ./...`)
- Vet: PASS (`go vet ./...`)
- Tests: PASS (3/3 packages with tests passed)

### #####################################################################

**2026-03-15T01:59:09Z**

**Dispatch: implement**
- **Grimnar** (generator): approved — 54s, $0.1653

**2026-03-15T01:59:13Z**

### Agent: Orchestrator (orchestrator) during verify @ 2026-03-15T01:59:13.080Z
### #####################################################################

PR created: https://github.com/EnderRealm/ticket/pull/5

### #####################################################################

**2026-03-15T02:00:02Z**

**Human answer:**

LGTM

**2026-03-15T02:00:50Z**

### Agent: Dvalin (impl-reviewer) during verify @ 2026-03-15T02:00:50.842Z
### #####################################################################

**Implementation Review (for human reference):**

## Implementation Review

### Acceptance Criteria

The ticket's acceptance criteria are implied by the description: *"Remove the migrate-beads reference from help text in cmd/root.go and README.md."*

- [x] **Remove `migrate-beads` from help text in `cmd/root.go`** — PASS: Line 83 (`migrate-beads              Import from .beads/issues.jsonl`) has been removed from the `helpText` string. The "Other:" section at line 81-82 now flows directly into "Filter flags" at line 84, with no `migrate-beads` reference remaining.
- [x] **Remove `migrate-beads` from README.md** — PASS (N/A): README.md has no `migrate-beads` reference. The implementer correctly noted this and made no unnecessary change.
- [x] **CHANGELOG.md historical reference left intact** — PASS: CHANGELOG.md line 285 retains the historical `migrate-beads` entry under the v0.1.2 changelog, which is appropriate — changelogs are historical records and should not be rewritten.

### Design Adherence

No formal design document was required for this docs-only chore ticket. The implementation follows the ticket description exactly:

- [x] Identified `cmd/root.go` line 83 as the target — followed
- [x] Checked README.md for references — followed, correctly found none
- [x] Left CHANGELOG.md historical entry untouched — followed, with documented rationale
- [x] Build/vet/test verification — followed (3/3 packages pass)

### External Type Verification

No external types involved — this is a docs-only string literal change.

### Scope

- **In scope coverage:** 2/2 files checked (cmd/root.go modified, README.md verified clean)
- **Scope creep:** None. Only the single help text line was removed.
- **Remaining references:** `migrate-beads` appears only in:
  - `CHANGELOG.md:285` — historical changelog entry (correct to keep)
  - `.tickets/deprecate-migrate-beads-d975.md` — this ticket itself
  - `.tickets/tic-303f.md` — the original ticket that decided not to port it

### Completeness Flags

- No TODO/FIXME/HACK comments
- No placeholder values or hardcoded constants
- No commented-out code
- No missing types or exports (N/A for this change)

### Summary

**Verdict: Approved.** This is a clean, minimal, single-line removal of a never-implemented command from the CLI help text. All in-scope items are addressed, README.md was correctly verified as already clean, and the CHANGELOG historical reference was appropriately preserved. Ready for merge.

### #####################################################################

**2026-03-15T02:00:50Z**

**Dispatch: verify**
- **Dvalin** (helper): approved — 45s, $0.1666
