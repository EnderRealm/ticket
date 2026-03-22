---
id: t-d60f
status: closed
deps: []
links: []
created: 2026-02-22T00:57:43Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# ID generation

Port the ID generation logic from bash to Go.

## Design

Files: pkg/ticket/id.go
Approach:
- GenerateID() string
- Extract directory name prefix: first letter of each hyphen/underscore segment
- Fallback to first 3 chars if no segments
- 4-char hex hash from sha256 of PID + timestamp
- Format: prefix-hash (e.g. t-5a3f)
- Use crypto/sha256 from stdlib, os.Getpid() for entropy

## Acceptance Criteria

Generated IDs match the pattern [a-z]+-[0-9a-f]{4}


## Notes

**2026-02-22T02:01:25Z**

Implemented: GenerateID/GenerateIDFrom, extractPrefix, idHash. Matches bash behavior: prefix from dir name segments, 4-char hex hash from sha256(pid+timestamp). Tests cover hyphenated, underscored, no-delimiter, and short dir names.
