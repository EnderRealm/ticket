---
id: t-4cfb
status: closed
deps: []
links: []
created: 2026-02-08T01:30:08Z
type: feature
priority: 4
assignee: Steve Macbeth
---
# Use real words from title for id to make it easier to track



## Notes

**2026-02-26T06:42:18Z**

## Brainstorm

**Decision:** (human) Drop project-level prefix entirely. Slug provides more context than a single letter ever did.

**Decision:** (human) ID format: `{slug}-{hash}` where slug is up to 3 meaningful words from the title, filler words stripped, no max length cap.

## Plan

1. Add a stop-word list to filter filler words (the, a, an, is, are, etc.)
2. Add a `slugify_title()` function that takes the title, lowercases it, strips non-alphanumeric chars, removes stop words, takes first 3 words, joins with hyphens
3. Modify `generate_id()` to accept the title as an argument and produce `{slug}-{hash}`
4. Update `cmd_create()` to pass title to `generate_id()`
5. Update help text if it references ID format
6. Update CHANGELOG.md

Existing tickets keep their old IDs. No migration needed — `ticket_path()` partial matching works regardless of format.

<!-- checkpoint: planning -->

**2026-02-26T06:47:44Z**

## Execute

Implemented in Go (the bash script is frozen):

- Rewrote `generate_id()` → `GenerateID(title)` to derive slug from title words
- Added stop-word list (~60 common filler words)
- `slugifyTitle()` lowercases, strips non-alnum, removes stop words, takes first 3 words
- Updated all call sites: `cmd/create.go`, `pkg/ticket/store.go`, `pkg/ticket/move.go`
- Added explicit duplicate-ID check in `Store.Create()`
- Rewrote test suite: 10 ID tests + 10-case table test for slugifyTitle
- All 98 package tests pass

<!-- checkpoint: executing -->
