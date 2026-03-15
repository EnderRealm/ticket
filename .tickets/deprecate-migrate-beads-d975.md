---
id: deprecate-migrate-beads-d975
stage: backlog
deps: []
links: []
created: 2026-03-12T19:04:38Z
type: chore
priority: 4
---
# Deprecate 'migrate-beads'

Remove the migrate-beads reference from help text in cmd/root.go and README.md. The command was never implemented — ticket tic-303f explicitly decided not to port it ("Will not build. Migrate-beads is a legacy one-time migration tool, not worth porting.").
