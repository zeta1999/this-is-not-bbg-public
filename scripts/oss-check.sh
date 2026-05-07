#!/bin/bash
# oss-check.sh — verify that an OSS-style cut works end-to-end.
#
# What it does:
#   1. Deploys the `reduced` plugin profile to a throw-away PLUGIN_DIR.
#   2. Builds the server, TUI, collector, schemacheck binaries.
#   3. Runs the Go test suite on server + tui + the deployed plugins.
#   4. Type-checks desktop + phone TypeScript.
#   5. Greps for plugin-specific identifiers leaking into core paths
#      (server/, tui/, desktop/src/, phone/src/, libs/pluginsdk/).
#
# Exits non-zero on any step failure, printing a summary line so a
# CI consumer can grep "OSS CHECK: FAIL".
#
# Usage:
#   ./scripts/oss-check.sh
#
# Honours TEST=skip / DT=skip / PH=skip env vars to skip specific
# steps when iterating locally.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# Throw-away plugin install dir so we don't disturb the user's
# configured ~/.config/notbbg/plugins.
TMP_PLUGIN_DIR="$(mktemp -d -t notbbg-oss-check-XXXXXX)"
trap 'rm -rf "$TMP_PLUGIN_DIR"' EXIT

PASS=0
FAIL=0
report() {
    if [ $FAIL -gt 0 ]; then
        echo "OSS CHECK: FAIL ($PASS passed, $FAIL failed)"
        exit 1
    fi
    echo "OSS CHECK: PASS ($PASS passed)"
}

run() {
    local label="$1"
    shift
    echo ""
    echo "=== $label ==="
    if "$@"; then
        PASS=$((PASS + 1))
        echo "  OK: $label"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: $label" >&2
    fi
}

# 1. Deploy reduced profile to the temp dir.
run "deploy reduced profile" \
    env PLUGIN_DIR="$TMP_PLUGIN_DIR" \
    ./scripts/deploy-plugins.sh --profile reduced --replace

# Sanity: the temp dir contains exactly the reduced set, nothing else.
EXPECTED=$(sort "$REPO_ROOT/scripts/plugin-profiles/reduced.txt" | grep -v '^#' | grep -v '^$' | tr '\n' ' ')
DEPLOYED=$(ls "$TMP_PLUGIN_DIR" 2>/dev/null | sort | tr '\n' ' ')
if [ "$EXPECTED" != "$DEPLOYED" ]; then
    echo "  FAIL: deployed set $DEPLOYED ≠ expected $EXPECTED" >&2
    FAIL=$((FAIL + 1))
fi

# 2. Build core binaries.
run "make build (server + tui + collector + schemacheck)" make build

# 3. Go tests on core (skips per-plugin tests since they live under examples/).
if [ "${TEST:-}" != "skip" ]; then
    run "go test ./... (server)" bash -c "cd server && go test ./... > /dev/null"
    run "go test ./... (tui)"    bash -c "cd tui    && go test ./... > /dev/null"
fi

# 4. Desktop + phone typecheck.
if [ "${DT:-}" != "skip" ]; then
    run "desktop tsc --noEmit" bash -c "cd desktop && npx tsc --noEmit"
fi
if [ "${PH:-}" != "skip" ]; then
    run "phone tsc --noEmit" bash -c "cd phone && npx tsc --noEmit"
fi

# 5. Plugin-isolation leak check. The unambiguous violation per
# project_oss_plugin_isolation.md is a core path importing a
# plugin package — that's what would break a clean OSS subset.
# Grep for `"github.com/notbbg/notbbg/examples/plugins/"` import
# statements outside the plugin tree itself. Comments and example
# URLs that mention plugin names are not leaks; they're docs.
echo ""
echo "=== plugin-isolation import check ==="
LEAKS=0
LEAK_OUTPUT="$(grep -rE '"github\.com/notbbg/notbbg/examples/plugins/[a-zA-Z_-]+"' \
        server/ tui/ desktop/src/ phone/src/ libs/pluginsdk/ \
        --include='*.go' --include='*.ts' --include='*.tsx' 2>/dev/null \
        | grep -v vendor/ \
        | grep -v node_modules/ || true)"

if [ -n "$LEAK_OUTPUT" ]; then
    echo "  LEAK: core path imports an examples/plugins/ package" >&2
    echo "$LEAK_OUTPUT" | head -10 >&2
    LEAKS=1
fi

if [ $LEAKS -gt 0 ]; then
    FAIL=$((FAIL + 1))
    echo "  FAIL: plugin-isolation leak(s) detected" >&2
else
    PASS=$((PASS + 1))
    echo "  OK: no core file imports an examples/plugins/ package"
fi

report
