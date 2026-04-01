#!/bin/bash
# local-test.sh — rebuild + restart everything for local testing
# Usage: ./scripts/local-test.sh
#
# Does: kill old processes → rebuild → init secrets → generate token →
#       start collector → pair → start TUI (which auto-starts server)

set -e

PASSWORD="${NOTBBG_PASSWORD:-notbbg-dev}"
COLLECTOR_CONFIG="server/configs/collector-local.yaml"
SECRETS_FILE="/tmp/collector-secrets.enc"
SESSION_FILE="/tmp/notbbg-collector-session.enc"

echo "=== Killing old processes ==="
pkill -9 notbbg 2>/dev/null || true
sleep 1

echo "=== Cleaning up stale files ==="
rm -f /tmp/notbbg.db /tmp/notbbg.sock /tmp/notbbg-collector.db
rm -f "$SECRETS_FILE" "$SESSION_FILE"
rm -rf /tmp/notbbg-datalake

echo "=== Building ==="
make build

echo "=== Init collector secrets ==="
NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg-collector \
    -init-secrets -enc-config "$SECRETS_FILE"

echo "=== Generating pairing token ==="
TOKEN=$(./bin/notbbg-collector -config "$COLLECTOR_CONFIG" -pair 2>/dev/null \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
echo "Token: ${TOKEN:0:16}..."

COLLECTOR_LOG="/tmp/notbbg-collector.log"

echo "=== Starting collector (logs: $COLLECTOR_LOG) ==="
NOTBBG_TOKEN="$TOKEN" NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg-collector \
    -config "$COLLECTOR_CONFIG" \
    -enc-config "$SECRETS_FILE" \
    > "$COLLECTOR_LOG" 2>&1 &
CPID=$!
echo "Collector PID: $CPID"
sleep 2

# Verify collector started.
if ! kill -0 $CPID 2>/dev/null; then
    echo "ERROR: Collector failed to start. Check $COLLECTOR_LOG"
    cat "$COLLECTOR_LOG"
    exit 1
fi

echo "=== Pairing TUI with collector ==="
NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg pair-collector localhost:9473 "$TOKEN"

echo ""
echo "=== Ready! Starting TUI ==="
echo "  Collector PID: $CPID"
echo "  Collector log: $COLLECTOR_LOG"
echo "  Server log:    visible in TUI LOG tab"
echo ""
NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg

echo ""
echo "=== TUI exited. Cleaning up ==="
kill -TERM $CPID 2>/dev/null
sleep 1
kill -9 $CPID 2>/dev/null
echo "Done."
