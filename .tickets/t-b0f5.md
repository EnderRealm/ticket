---
id: t-b0f5
status: open
deps: []
links: []
created: 2026-02-21T23:27:24Z
type: bug
priority: 1
assignee: Steve Macbeth
---
# stats: broken on macOS due to awk mktime dependency

tk stats fails with exit 2 and no output on macOS because the awk script uses mktime(), a gawk-only function. BSD awk (shipped with macOS) does not support it. The entire command is unusable without installing gawk. Need to replace mktime with a portable date calculation or shell-side epoch computation.

