#!/usr/bin/env bash
# tk store migration: cutover script for MacBook.
#
# Run on MacBook after Studio cutover is verified. Studio has already pushed
# the unified ticket history to origin/main of EnderRealm/forge-data and
# extracted it to EnderRealm/forge-tickets.
#
# Adjust FORGE_DATA and STORE if your paths differ.

set -euo pipefail

FORGE_DATA="${FORGE_DATA:-$HOME/code/forge-data}"
STORE="${STORE:-$HOME/code/tickets-store}"
CONFIG="$HOME/.ticket/config.yaml"

echo "==> stop tk serve"
pkill -TERM -f 'tk serve' || true
sleep 2
if pgrep -f 'tk serve' >/dev/null; then
  echo "tk serve still running; aborting" >&2
  exit 1
fi

if [[ -d "$FORGE_DATA/.git" ]]; then
  echo "==> reconcile forge-data (push any local commits)"
  cd "$FORGE_DATA"
  STASHED=0
  if ! git diff --quiet || ! git diff --cached --quiet; then
    git stash push -u -m "macbook-cutover" >/dev/null
    STASHED=1
  fi
  git fetch origin
  if [[ -n "$(git log origin/main..HEAD --oneline 2>/dev/null)" ]]; then
    git pull --rebase
    git push origin main
  else
    git pull --rebase
  fi
  if [[ $STASHED -eq 1 ]]; then
    git stash pop || echo "stash pop conflict — resolve manually after cutover"
  fi
  cd - >/dev/null
fi

echo "==> clone forge-tickets to $STORE"
if [[ -e "$STORE" ]]; then
  echo "$STORE already exists; skipping clone" >&2
else
  git clone git@github.com:EnderRealm/forge-tickets.git "$STORE"
fi

echo "==> repoint central_root in $CONFIG"
if [[ ! -f "$CONFIG" ]]; then
  echo "$CONFIG missing; aborting" >&2
  exit 1
fi
cp "$CONFIG" "$CONFIG.bak.$(date +%s)"
sed -i.tmp "s|^central_root: .*|central_root: $STORE|" "$CONFIG"
rm -f "$CONFIG.tmp"

echo "==> verify"
grep '^central_root:' "$CONFIG"
tk list 2>&1 | head -5

echo "==> done. Restart tk serve in your terminal (or let Claude Code respawn it)."
