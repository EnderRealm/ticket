---
id: t-7cc7
status: closed
deps: [t-ef3c]
links: []
created: 2026-02-20T23:55:56Z
type: task
priority: 2
assignee: Steve Macbeth
parent: t-571a
---
# CI workflow for tests

Create GitHub Actions workflow to run BATS tests on push to master and PRs.

## Design

File: .github/workflows/test.yml
Matrix: ubuntu-latest + macos-latest (covers platform portability: sha256sum vs shasum, gawk vs mawk, GNU vs BSD sed).
Steps: checkout with submodules:true, install jq on Linux, run ./test/bats/bin/bats test/*.bats, run perf with continue-on-error:true and PERF_MULTIPLIER=5.
Trigger: push to master + pull_request.

## Acceptance Criteria

Workflow runs on both ubuntu and macos. Tests execute using vendored bats from submodule. Perf failures don't block CI.

