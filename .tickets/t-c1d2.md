---
id: t-c1d2
status: open
deps: []
links: []
created: 2026-02-15T23:10:48Z
type: feature
priority: 2
assignee: Steve Macbeth
---
# Add tk testing command to list needs_testing tickets

Add a 'tk testing' command that lists all tickets with status needs_testing. Provides a quick way to see what's waiting for human verification.

## Design

File: ticket (single bash script)

Approach: New command in the case-statement dispatch. Equivalent to 'tk ls --status=needs_testing' but shorter and more discoverable.

Implementation:
1. Add 'testing' case to command dispatch
2. Delegate to the existing ls logic with --status=needs_testing pre-applied
3. Accept the same filter flags as ls (--type, --priority, --assignee, --tag)
4. Add to help text under Viewing section

## Acceptance Criteria

tk testing lists all needs_testing tickets. tk testing --type bug filters to bugs only. Shows in help output.

