#!/usr/bin/env bash
# Runs the steps of .github/workflows/lint.yml locally, in the same order, and
# stops at the first one that fails, naming it. It is the preCommit hook in
# .claude/work-hooks.json, so a commit CI would reject is refused before it is
# made. Runner-only failures, such as a race that needs CI's load, still need
# the CI run itself.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

fail() {
  echo "ci-local: $1 failed" >&2
  exit 1
}

echo "==> gofmt"
unformatted="$(gofmt -l .)" || fail gofmt
if [ -n "$unformatted" ]; then
  echo "These files are not gofmt-formatted:"
  echo "$unformatted"
  echo "Run: gofmt -w ."
  fail gofmt
fi

echo "==> go vet"
go vet ./... || fail "go vet"

echo "==> go test"
go test ./... || fail "go test"

echo "ci-local: all steps passed"
