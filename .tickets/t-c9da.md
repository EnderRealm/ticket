---
id: t-c9da
status: open
deps: []
links: []
created: 2026-02-21T23:06:57Z
type: bug
priority: 3
assignee: Steve Macbeth
---
# add-note: reject empty note content from stdin

When stdin is not a TTY and provides empty content (e.g. echo '' | tk add-note <id>), the command adds an empty note instead of failing. Should check for empty content after reading stdin and fail with 'no note provided' like the TTY case.

