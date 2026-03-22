---
id: extract-hardcoded-config-a41b
stage: backlog
deps: []
links: []
created: 2026-03-22T19:42:48Z
type: feature
priority: 2
parent: eb6c
tags: [architecture, tk, storage]
---
# Extract hardcoded config values to ~/.ticket/config.yaml

Audit tk for hardcoded configuration values and move them to ~/.ticket/config.yaml as top-level fields.

Currently `central_root` is the only configurable top-level field. The default fallback path (`~/code/forge-data/tickets`) is still hardcoded in `CentralStoreRoot()`. Other candidates:

- Default store type (central vs local) for `tk init`
- Git identity for central store bootstrap (currently hardcoded as tk@local / tk)
- Any other values that surface during the audit

The goal is that all behavioral defaults live in the config file, with hardcoded values only as last-resort fallbacks.
