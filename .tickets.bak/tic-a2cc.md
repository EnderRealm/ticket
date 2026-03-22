---
id: tic-a2cc
status: closed
deps: [tic-c930]
links: []
created: 2026-02-26T04:33:31Z
type: task
priority: 1
assignee: Steve Macbeth
parent: tic-46c0
tags: [redesign, pipeline]
---
# Add migration logic in pkg/ticket/migrate.go





New file pkg/ticket/migrate.go. MigrateTicket() converts status-based tickets to stage-based: open→triage, in_progress→implement, needs_testing→test, closed→done. NeedsMigration() check. Idempotent — already-migrated tickets are skipped.
