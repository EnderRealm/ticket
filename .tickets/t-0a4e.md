---
id: t-0a4e
status: open
deps: []
links: []
created: 2026-02-15T23:05:16Z
type: feature
priority: 2
assignee: Steve Macbeth
---
# Add tk projects command to list configured projects

Add a 'tk projects' command that lists all configured projects from the manager's config.json. Provides quick visibility into available projects for use with --project flag.

## Design

File: ticket (single bash script ~2150 lines)

Approach: New command in the case-statement dispatch that reads manager's config.json.

Implementation:
1. Add TK_MANAGER_DIR environment variable support near top of script (alongside TICKETS_DIR)
   - TK_MANAGER_DIR="${TK_MANAGER_DIR:-}" (no default, must be set)
2. Add 'projects' case to the command dispatch
3. When invoked:
   - Check TK_MANAGER_DIR is set, error if not
   - Read $TK_MANAGER_DIR/config.json
   - Parse the projects array (jq or simple grep/sed since it's a simple JSON structure)
   - Display each project name and path
   - Optionally show ticket count per project
4. Consider dependency: jq may not be available everywhere. Use jq if present, fall back to grep/awk parsing of the simple JSON structure.

Manager config.json format:
{
  "projects": [
    { "name": "Manager", "path": "/Users/smacbeth/code/manager" },
    ...
  ]
}

Output format:
  Manager  /Users/smacbeth/code/manager
  Ticket   /Users/smacbeth/code/ticket

## Acceptance Criteria

TK_MANAGER_DIR=~/code/manager tk projects lists all projects from config.json with name and path. Missing env var prints helpful error. Missing or invalid config.json prints error.

