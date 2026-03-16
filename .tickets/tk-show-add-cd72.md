---
id: tk-show-add-cd72
stage: done
risk: low
deps: []
links: []
created: 2026-03-15T05:46:36Z
type: feature
priority: 3
branch: forge/tk-show-add-cd72
run_id: orch-mmsq8e9e-yy4lns
---
# 'tk show' add support for --metadata

--metadata should only return frontmatter fields and description, no other notes

## Review Log

**2026-03-16T05:35:30Z [human:tui]**
APPROVED — Looks good.

## Notes

**2026-03-15T22:44:17Z**

## Triage

**Risk:** low — Isolated change to `cmd/show.go`, adds a new non-breaking flag. No auth/PII/schema/API contract impact.

**Priority:** 3 — Convenience feature, not blocking other work.

**Scope:** Single task. Add `--metadata` bool flag to show command to filter output to frontmatter + description only (omit notes, reviews, relationship sections).

**Key decisions:**
- Risk low, priority 3 confirmed (human)

**2026-03-16T05:14:48Z**

**Dispatch: spec**
- **Rundar** (generator): approved — 150s, $0.5229

**2026-03-16T05:18:31Z**

**Dispatch: design**
- **Helga** (generator): approved — 223s, $0.7399

**2026-03-16T05:22:33Z**

### Agent: Grimnar (surgical-coder) during implement @ 2026-03-16T05:22:33.520Z
### #####################################################################

## Summary

### What changed and why

Added `--metadata` flag to the `tk show` command to allow displaying only frontmatter fields and description, omitting notes, reviews, and relationship sections. This is a convenience feature for when users need just the core ticket metadata.

### Files modified

1. **`cmd/show.go`** (lines 19-54):
   - Added `--metadata` bool flag registration in `init()`
   - Read flag in `runShow`, passed to `showTicket` as new `metadataOnly` parameter
   - When `metadataOnly=true`: creates a shallow copy of the ticket with nil Notes/Reviews, serializes it (producing only frontmatter + title + body), and returns early — skipping all relationship sections

2. **`cmd/create.go`** (line 134):
   - Updated `showTicket` call to pass `false` for the new `metadataOnly` parameter

3. **`cmd/root.go`** (line 23):
   - Updated help text to show `show <id> [--metadata]`

4. **`README.md`** (line 85):
   - Updated usage section to show `show <id> [--metadata]`

5. **`CHANGELOG.md`** (lines 3-5):
   - Added `[Unreleased]` section with the new flag entry

### Verification results

- **Build**: PASS
- **Tests**: All relevant tests PASS. One pre-existing failure in `TestParse_RealTicketFiles` due to corrupted `tk-ui-cursor-cc4b.md` file (not related to this change)
- **Smoke test**: `tk show --metadata tk-show-add-cd72` correctly outputs only frontmatter + description; `tk show tk-show-add-cd72` retains full output with notes and relationships

### #####################################################################

**2026-03-16T05:22:34Z**

**Dispatch: implement**
- **Grimnar** (generator): approved — 242s, $1.2027

**2026-03-16T05:25:24Z**

**Dispatch: test**
- **Sindri** (generator): approved — 169s, $0.9971

**2026-03-16T05:25:27Z**

### Agent: Orchestrator (orchestrator) during verify @ 2026-03-16T05:25:27.592Z
### #####################################################################

PR created: https://github.com/EnderRealm/ticket/pull/8

### #####################################################################

**2026-03-16T05:25:27Z**

## Awaiting Human Input

**Stage:** verify
**Action needed:** Stage verify requires human review
**PR:** https://github.com/EnderRealm/ticket/pull/8
**Run:** orch-mmsq8e9e-yy4lns
