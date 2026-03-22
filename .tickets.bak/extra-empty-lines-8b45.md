---
id: extra-empty-lines-8b45
stage: done
status: open
deps: []
links: []
created: 2026-03-01T01:32:38Z
type: bug
priority: 0
---
# Extra empty lines in the note field










Every store.Update() call triggers a parse→serialize round-trip. In parseBody (format.go:236), the body is trimmed with TrimRight (trailing newlines only), but the serializer adds a \n before the body (line 101). This means each round-trip prepends one additional blank line to the body. After N updates, the file has N blank lines between the title and description.\n\nRoot cause: parseBody line 236 uses strings.TrimRight instead of strings.TrimSpace.\nFix: change to strings.TrimSpace to trim both leading and trailing whitespace.

## Test Results

- [x] TestSerialize_BodyNoBlankLineAccumulation: 10 round-trips with no blank line accumulation\n- [x] All existing tests pass: go test ./... green\n- [x] Fix is one-line change in parseBody: TrimRight → TrimSpace

## Review Log

**2026-03-01T06:09:16Z [agent:impl-reviewer]**
APPROVED — Root cause fix in format.go:236 correct. TrimSpace eliminates accumulated leading newlines. Test covers 10 round-trips with Notes section intact. No scope creep.

**2026-03-01T06:09:20Z [agent:code-reviewer]**
APPROVED — Fix is correct and minimal. TrimSpace is the right idiom. Test catches accumulation class. Suggestions: tighter positive assertion on exact blank line count, and starting round-trip from leading-newline form for more direct reproduction. Neither blocks merge.

**2026-03-01T06:10:39Z [human:steve]**
APPROVED — Verified fix.
