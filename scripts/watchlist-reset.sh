#!/usr/bin/env bash
# watchlist-reset.sh — overwrite server/configs/watchlist.yaml with the
# sanitized example baseline.
#
# Use this before screen-sharing, demoing, or capturing a recording —
# anything that might expose the file. The personal watchlist.yaml is
# gitignored so commits don't leak it, but a `cat` over the operator's
# shoulder will.
#
# Usage:
#   ./scripts/watchlist-reset.sh
#
# Idempotent. Safe to run repeatedly.

set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$REPO_ROOT/server/configs/watchlist.example.yaml"
DST="$REPO_ROOT/server/configs/watchlist.yaml"

if [ ! -f "$SRC" ]; then
    echo "error: missing example file at $SRC" >&2
    exit 1
fi

cp "$SRC" "$DST"
echo "watchlist reset → $DST"
echo "  source: $(basename "$SRC")"
echo "  companies: $(grep -c '^  - name:' "$DST")"
echo
echo "Restart the server to pick up the change:"
echo "  pkill notbbg-server && make run-server-oss"
