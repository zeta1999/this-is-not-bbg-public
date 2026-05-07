#!/usr/bin/env bash
# watchlist-from-md.sh — regenerate server/configs/watchlist.yaml from a
# propaganda/investment markdown watchlist.
#
# Thin wrapper around scripts/watchlist-from-md.py. With no argument it
# picks the most recent ../propaganda/investment/company-watchlist-*.md
# (lexicographic sort works because the filenames are ISO-dated). Pass
# an explicit path to override.
#
# Usage:
#   ./scripts/watchlist-from-md.sh                       # auto-pick latest
#   ./scripts/watchlist-from-md.sh path/to/watchlist.md  # explicit
#
# The python script is run with --write, so server/configs/watchlist.yaml
# is overwritten in place (a .bak is left behind by the python side).

set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PY="$REPO_ROOT/scripts/watchlist-from-md.py"
INV_DIR="$REPO_ROOT/../propaganda/investment"

if [ ! -x "$PY" ]; then
    echo "error: $PY missing or not executable" >&2
    exit 1
fi

if [ $# -ge 1 ]; then
    MD="$1"
else
    MD="$(ls -1 "$INV_DIR"/company-watchlist-*.md 2>/dev/null | sort | tail -n1)"
    if [ -z "$MD" ]; then
        echo "error: no company-watchlist-*.md found in $INV_DIR" >&2
        echo "       pass an explicit path as the first argument" >&2
        exit 1
    fi
    echo "auto-picked: $(basename "$MD")"
fi

if [ ! -f "$MD" ]; then
    echo "error: $MD does not exist" >&2
    exit 1
fi

"$PY" "$MD" --write

echo
echo "Restart the server to pick up the change:"
echo "  pkill notbbg-server && make run-server-oss"
