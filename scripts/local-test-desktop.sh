#!/bin/bash
# local-test-desktop.sh — rebuild + start everything including desktop app
# Usage: ./scripts/local-test-desktop.sh
#
# Starts: collector → TUI (auto-starts server) + Tauri desktop app

set -e

PASSWORD="${NOTBBG_PASSWORD:-notbbg-dev}"
COLLECTOR_CONFIG="server/configs/collector-local.yaml"
SECRETS_FILE="/tmp/collector-secrets.enc"
SESSION_FILE="/tmp/notbbg-collector-session.enc"
COLLECTOR_LOG="/tmp/notbbg-collector.log"
TAURI_LOG="/tmp/notbbg-tauri.log"

echo "=== Killing old processes ==="
pkill -9 notbbg 2>/dev/null || true
pkill -f "vite" 2>/dev/null || true
sleep 1

echo "=== Cleaning up stale files ==="
rm -f /tmp/notbbg.db /tmp/notbbg.sock /tmp/notbbg-collector.db
rm -f "$SECRETS_FILE" "$SESSION_FILE"
rm -rf /tmp/notbbg-datalake

echo "=== Building Go binaries ==="
make build

echo "=== Deploying plugins ==="
./scripts/deploy-plugins.sh

echo "=== Init collector secrets ==="
NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg-collector \
    -init-secrets -enc-config "$SECRETS_FILE"

echo "=== Generating pairing token ==="
TOKEN=$(./bin/notbbg-collector -config "$COLLECTOR_CONFIG" -pair 2>/dev/null \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
echo "Token: ${TOKEN:0:16}..."

echo "=== Starting collector (logs: $COLLECTOR_LOG) ==="
NOTBBG_TOKEN="$TOKEN" NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg-collector \
    -config "$COLLECTOR_CONFIG" \
    -enc-config "$SECRETS_FILE" \
    > "$COLLECTOR_LOG" 2>&1 &
CPID=$!
sleep 2

if ! kill -0 $CPID 2>/dev/null; then
    echo "ERROR: Collector failed to start. Check $COLLECTOR_LOG"
    cat "$COLLECTOR_LOG"
    exit 1
fi
echo "Collector PID: $CPID"

echo "=== Pairing TUI with collector ==="
NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg pair-collector localhost:9473 "$TOKEN"

echo "=== Installing desktop dependencies ==="
cd desktop
npm install --silent 2>/dev/null
cd ..

DESKTOP_LOG="/tmp/notbbg-desktop.log"

echo "=== Starting Vite dev server ==="
(cd desktop && npm run dev > "$DESKTOP_LOG" 2>&1) &
sleep 3

echo "=== Starting Electron desktop app ==="
(cd desktop && VITE_DEV_URL=http://localhost:1420 npx electron . > /tmp/notbbg-electron.log 2>&1) &
TPID=$!

echo ""
echo "============================================"
echo "  Starting!"
echo ""
echo "  TUI:       launching now (this terminal)"
echo "  Desktop:   Electron window"
echo "  Server:    auto-started by TUI"
echo "  Collector: PID $CPID"
echo ""
echo "  Logs:"
echo "    Collector:  tail -f $COLLECTOR_LOG"
echo "    Desktop:    tail -f $DESKTOP_LOG"
echo "    Electron:   tail -f /tmp/notbbg-electron.log"
echo "    Server:     TUI LOG tab (key 6)"
echo "============================================"
echo ""

NOTBBG_PASSWORD="$PASSWORD" ./bin/notbbg

echo ""
echo "=== TUI exited. Cleaning up ==="
kill -TERM $CPID $TPID 2>/dev/null
sleep 1
kill -9 $CPID $TPID 2>/dev/null
pkill -f "vite" 2>/dev/null || true
echo "Done."
