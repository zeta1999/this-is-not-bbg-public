# TESTING.md — Manual Testing Guide

## Prerequisites

```bash
make build          # builds bin/notbbg-server, bin/notbbg-collector, bin/notbbg
```

## Common gotchas (read first)

### "Password for collector localhost:9473:" prompt on TUI launch

The TUI is asking for the **vault password** — the one you used
with `notbbg-collector -init-secrets` (or
`notbbg-server -init-secrets`). It's needed to decrypt a
previously-paired collector token stored in
`~/.config/notbbg/config.yaml` under `server.collector_token`.

The collector is **optional** — it's the remote-backup component
that mirrors the bus to a remote machine's datalake over PQC TLS.
For local testing you usually don't need it.

Three ways through the prompt:

1. **Type the vault password** you set with `-init-secrets`.
2. **Bypass via env var:** `NOTBBG_PASSWORD='<pwd>' make run-tui`.
3. **Drop the collector pairing** (cleanest if you're not using
   it):
   ```bash
   cp ~/.config/notbbg/config.yaml ~/.config/notbbg/config.yaml.bak
   python3 -c "
   import yaml; p='$HOME/.config/notbbg/config.yaml'
   c=yaml.safe_load(open(p)) or {}
   c.setdefault('server',{}).pop('collector_addr',None)
   c.setdefault('server',{}).pop('collector_token',None)
   open(p,'w').write(yaml.dump(c))
   "
   ```

### Is the collector running?

```bash
nc -z localhost 9473 && echo open || echo closed
pgrep -fl notbbg-collector
```

The collector lives at port 9473 (server is at 9474). They're
separate processes and `make run-server` does **not** start the
collector. See §5 below for how to start it if you want remote
backup.

### `make smoke` on macOS prints "port 9474 open (TLS handshake skipped)"

That's expected. macOS system curl (LibreSSL/3.3.6) can't
negotiate the server's self-signed TLS 1.3 cert, so the smoke
target falls through to a bare port check. Server is fine — the
actual GUIs use modern TLS stacks (Electron Chromium,
react-native-sse, Go's crypto/tls) and connect cleanly.

---

## 1. Server Startup

```bash
./bin/notbbg-server -config server/configs/dev.yaml
```

**Expected output:**
- Registered adapters: binance, okx, bybit, bitget, coingecko, yahoo_finance, rss, uniswap_v3, hyperliquid, gmx, dydx, drift, serum, raydium, jupiter
- `binance connected streams=9`
- Backfill messages for BTCUSDT, ETHUSDT, SOLUSDT (4 timeframes each)
- `binance stream stats klines=N trades=N depth=N` (every 30s)
- `datalake stats written=N` (every 60s)
- `cron job completed job=cache-stats` (every 5m)

---

## 2. TUI (Terminal UI)

```bash
./bin/notbbg
```

### Test each panel

| Key | Panel | What to check |
|-----|-------|--------------|
| `1` | OHLC | Candlestick chart, price/change, timeframe `[1m] 5m 1h 1d`, sidebar |
| `2` | LOB | Bids/asks aligned columns, spread, sidebar with instrument switching |
| `3` | NEWS | Headlines from 20+ RSS feeds, scrollable, filterable |
| `4` | ALERTS | Alert creation and display |
| `5` | MON | Feed status grid: all exchanges, DEXes, world feeds |
| `6` | LOG | Server log lines (green INFO, yellow WARN, red ERROR) |
| `7` | AGENT | Agent skill list, scrollable output area |

### Test OHLC panel
- `[` / `]` or `←` / `→` — cycle instruments (Binance, OKX, Bybit, Bitget, CoinGecko, Yahoo, Uniswap, Hyperliquid)
- `-` / `+` — cycle timeframes
- `/SOL` — search for SOL instrument
- `/nikkei` — jump to Nikkei 225 (Yahoo Finance)
- `/hyperliquid` — jump to Hyperliquid instruments
- Sidebar scrolls to keep active instrument visible

### Test LOB panel
- `[` / `]` — cycle between order books (Binance, OKX, Bybit, Bitget, Hyperliquid)
- Verify aligned columns, spread display

### Test NEWS panel
- `j` / `k` or `↑` / `↓` — navigate headlines
- `Enter` — open article detail (body, URL, tickers)
- `/BTC` — filter by keyword
- `ESC` — clear filter or close article
- Sources: CoinTelegraph, CoinDesk, Decrypt, The Block, CNBC, Bloomberg, FT, X.com (Nitter)

### Test commands

```
h or ?        → help overlay (ESC to close)
/BTC          → OHLC search (on OHLC panel) or news filter (on NEWS panel)
/LOB          → switch to LOB panel
/AGENT        → switch to Agent panel
/ALERT SET BTCUSDT > 100000   → price alert
/PAIR         → pairing URL
```

### Test server lifecycle
1. Start TUI — auto-starts server
2. `pkill notbbg-server` → TUI reconnects, restarts server
3. `q` → TUI exits, kills managed server

### Test backpressure (LOG tab)
- After 30s: `client relay stats realtime_sent=N bulk_sent=N realtime_drop=0`
- Credits: `bulk_sent` should grow steadily as TUI sends credit messages

---

## 3. CLI Commands

```bash
# Export OHLC data
./bin/notbbg export ohlc BTCUSDT -e binance -f csv -n 5

# Search news
./bin/notbbg news search BTC --limit 10

# List feeds
./bin/notbbg feeds list

# Query history
./bin/notbbg history BTCUSDT -e binance -n 10

# List agent skills
./bin/notbbg agent list

# Plugin management
./bin/notbbg plugin list
```

---

## 4. Collector (Remote Data Agent)

```bash
# Generate pairing token
./bin/notbbg-collector -config server/configs/dev.yaml -pair

# Start collector
./bin/notbbg-collector -config server/configs/dev.yaml
```

**Expected:** TCP+TLS listener on :9473, all feeds running, datalake writing.

---

## 5. Remote Collector (Data Backup Service)

The **server** grabs all market data and pushes it to a remote **collector**.
The collector is a passive receiver that writes to a local datalake.
The TUI connects to the local server via unix socket (no pairing needed).

```
Server (local)  ──TLS+PQC──►  Collector (remote)
  ├── feeds                      └── datalake writer
  ├── serves TUI (unix socket)
  └── serves phone (HTTP/SSE)
```

### 5a. Local test (everything on one machine)

```bash
# 1. Init encrypted secrets (one-time each)
./bin/notbbg-collector -init-secrets -enc-config /tmp/collector-secrets.enc
# → prompts for password

./bin/notbbg-server -init-secrets -enc-config configs/secrets.enc
# → prompts for password

# 2. Generate pairing token (single-use, 10min TTL)
TOKEN=$(./bin/notbbg-collector -config server/configs/collector-local.yaml -pair 2>/dev/null \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
echo "Token: $TOKEN"

# 3. Start collector with token + encrypted secrets
NOTBBG_TOKEN=$TOKEN NOTBBG_PASSWORD=<password> ./bin/notbbg-collector \
  -config server/configs/collector-local.yaml \
  -enc-config /tmp/collector-secrets.enc &
sleep 3

# 4. Pair TUI with collector (one-time, saves encrypted to config)
./bin/notbbg pair-collector localhost:9473 $TOKEN
# → prompts: "Set a password to encrypt the collector token: "

# 5. Start TUI (auto-starts server with secrets + collector push)
NOTBBG_PASSWORD=<password> ./bin/notbbg
# → auto-starts server with -collector localhost:9473 -collector-token <decrypted>
# → server connects to collector via TLS+PQC, authenticates
# → data flows: feeds → server → TUI (local) + collector (datalake)

# 6. In another terminal, verify datalake:
find /tmp/notbbg-datalake -type f | wc -l
# Expected: 100+ files within 30 seconds

head -1 /tmp/notbbg-datalake/type=ohlc/exchange=binance/instrument=BTCUSDT/year=*/month=*/day=*/data.jsonl

# 7. Cleanup
pkill notbbg
rm -rf /tmp/notbbg-datalake /tmp/notbbg-collector.db /tmp/notbbg.db \
       /tmp/notbbg.sock /tmp/collector-secrets.enc
```

### 5b. Ajax test (remote collector)

```bash
# === ON AJAX ===

# 1. Init secrets (one-time)
./bin/notbbg-collector -init-secrets -enc-config ~/notbbg/secrets.enc
# → prompts for password

# 2. Generate token
TOKEN=$(./bin/notbbg-collector -config configs/collector-ajax.yaml -pair 2>/dev/null \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
echo "Token: $TOKEN"
# → communicate token out-of-band (SSH, secure chat, in person)

# 3. Start collector
NOTBBG_TOKEN=$TOKEN NOTBBG_PASSWORD=<password> ./bin/notbbg-collector \
  -config configs/collector-ajax.yaml -enc-config ~/notbbg/secrets.enc &

# === ON YOUR MACHINE ===

# 4. Init server secrets (one-time)
./bin/notbbg-server -init-secrets -enc-config configs/secrets.enc
# → prompts for password

# 5. Pair with ajax collector (one-time)
./bin/notbbg pair-collector ajax:9473 <token>
# → prompts for password to encrypt token

# 6. Start TUI (everything automatic)
NOTBBG_PASSWORD=<password> ./bin/notbbg
# → auto-starts server, pushes data to ajax collector

# 7. Verify datalake on ajax:
ssh ajax 'find ~/notbbg/datalake -type f | wc -l'
```

### 5c. Security verification

```bash
# Wrong token — must be rejected:
./bin/notbbg-server -config server/configs/dev.yaml \
  -collector localhost:9473 -collector-token WRONG-TOKEN
# Expected: "pairing rejected: invalid pairing token"

# No PQC handshake — raw TCP connect fails:
# (the collector requires PQC before accepting any commands)

# Expired token — must be rejected:
# (wait 10+ minutes after generating token, then try to connect)
# Expected: "pairing rejected: pairing token expired"

# Token reuse — must be rejected:
# (use same token twice)
# Expected: "pairing rejected: pairing token already used"
```

### 5d. Forget collector pairing

```bash
./bin/notbbg pair-collector --forget
# → removes saved collector from config
# → next 'notbbg' runs server without collector push
```

### 5e. Security layers

The connection uses three security layers:
1. **TLS 1.3** — classical encryption (ECDHE + AES-256-GCM)
2. **ML-KEM-768** — post-quantum key exchange (1184-byte pubkey + 1088-byte ciphertext)
3. **Argon2id + XChaCha20** — encrypted secrets at rest

See [SECURITY.md](SECURITY.md) for the full security model.

---

## 6. Datalake Verification

After running server for 1+ minutes:

```bash
find /tmp/notbbg-datalake -type f | head -20
# Expected: Hive-partitioned structure:
# type=ohlc/exchange=binance/instrument=BTCUSDT/year=2026/month=03/day=29/data.jsonl

head -1 /tmp/notbbg-datalake/type=ohlc/exchange=binance/instrument=BTCUSDT/year=*/month=*/day=*/data.jsonl
# Expected: JSON record with _topic, _timestamp, payload
```

---

## 7. HTTP/SSE Gateway

```bash
curl http://localhost:9474/api/v1/health
curl http://localhost:9474/api/v1/snapshot?topic=ohlc.coingecko.*&limit=3
curl -N http://localhost:9474/api/v1/subscribe?patterns=ohlc.*.*
open http://localhost:9474/api/v1/pair/qr
```

---

## 8. Desktop App (Electron)

```bash
# Start server in background
./bin/notbbg-server -config server/configs/dev.yaml &

# Start desktop app (Vite dev server + Electron)
cd desktop && npm install && npm run electron
```

Alternatively, for development with hot-reload:
```bash
cd desktop && npm run dev          # Vite at http://localhost:1420
# In another terminal:
cd desktop && npm run electron:dev # Electron connects to Vite dev server
```

### Test each tab

| Key | Tab | What to check |
|-----|-----|--------------|
| `1` | OHLC | Candlestick chart (lightweight-charts), sidebar, timeframe buttons |
| `2` | LOB | Order book with bid/ask columns, spread, depth bars |
| `3` | NEWS | Headlines with server-side BM25 search, article detail view |
| `4` | ALERTS | Alert list |
| `5` | MON | Feed status grid for all exchanges and DEXes |
| `6` | LOG | Server log lines (color-coded by level) |
| `7` | AGENT | Claude agent via HTTP endpoint |
| `8+` | Plugins | Dynamic tabs from installed plugins |

### Test keyboard shortcuts
- `1`-`9` — jump to tab
- `TAB` / `Shift+TAB` — cycle tabs
- `[` / `]` — cycle instruments (OHLC, LOB)
- `-` / `+` — cycle timeframes (OHLC)

### Test SSE connection
- Bottom bar shows `CONNECTED` with green dot when SSE stream is active
- Data starts flowing within 2-3 seconds of connection
- Disconnect server → bottom bar shows `DISCONNECTED` with red dot

### Test phone pairing
- Click phone icon (top right) → pairing modal with QR code
- Scan with phone app → phone connects to server

---

## 9. Phone App (Expo)

```bash
./bin/notbbg-server -config server/configs/dev.yaml &
cd phone && npm install && npx expo start
```

**6 tabs:** Watchlist, LOB, News, Alerts, Plugins, Settings

---

## 10. Security

```bash
# Test NOTBBG_SECRET_FILE
echo "mysecret123" > /tmp/notbbg-secret
NOTBBG_SECRET_FILE=/tmp/notbbg-secret ./bin/notbbg-server -config server/configs/dev.yaml
# Expected: "seeded secret from file"

# Test encrypted config
NOTBBG_PASSWORD=test ./bin/notbbg-server -config server/configs/dev.yaml -enc-config /path/to/encrypted.yaml
```

---

## 11. Distribution Build

```bash
make dist
# Expected: dist/{darwin-arm64,linux-amd64,linux-arm64,windows-amd64}/
# Each contains: notbbg, notbbg-server, notbbg-collector, configs/dev.yaml
```

---

## 12. Unit + Integration Tests

```bash
make test
```

---

## What should work after 2026-04-23 tracks (file feeds + GetDataRange + phone ANR fix)

The three tracks shipped on 2026-04-23 add runtime wiring for
previously orphaned Phase 3 adapters, expose GetDataRange over HTTP,
and fix a known HIGH-severity phone ANR. Below is the end-to-end
walkthrough you should run before signing off.

### A. Baseline: all three GUIs still connect

```bash
make build
./bin/notbbg-server -config server/configs/dev.yaml &
```

| GUI | Command | Pass criteria |
|-----|---------|---------------|
| TUI | `./bin/notbbg` | All 7 panels render; keys `1`-`7` cycle; `/BTC` searches |
| Desktop | `cd desktop && npm install && npm run electron` | Main window loads; status pill shows CONNECTED; OHLC + TRADES tabs populate within 10s |
| Phone | `cd phone && npm install && npx expo start` | Expo dev menu; QR pair, OHLC + TRADES + MON tabs load |

### B. Phone TRADES ANR regression check

Before: Android "Process system isn't responding" at ~104 trades/s on BTCUSDT.
After the fix, the TRADES screen should hold steady.

1. Boot an Android emulator (fresh wipe if reproducing old bug).
2. Pair the phone, open the **TRADES** tab, select BTCUSDT/binance.
3. Leave it running for 5+ minutes.

Pass:
- No ANR dialog.
- Recent trades list tops out at ~30 rows (MAX_VISIBLE_TRADES) — older
  rows roll off as new ones arrive.
- Instrument switcher (horizontal chips at the top) remains responsive.
- JS frame time stays ≤ 16ms for scrolling in the Perf monitor.

Fail:
- ANR returns → revert to `git log` commit before the FlatList switch
  and file a repro case. Look for render spikes in React Native's
  Performance monitor.

### C. File-based feeds (Sibelius, Ravel, ts-base) end-to-end

These adapters are disabled in `dev.yaml`. To exercise:

1. Create drop directories:
   ```bash
   mkdir -p /tmp/notbbg-sibelius /tmp/notbbg-ravel /tmp/notbbg-tsbase
   ```
2. Copy sample fixtures:
   ```bash
   cp reference/Sibelius/TestData/inputs/aria_calibrate_simple.json \
      /tmp/notbbg-sibelius/
   cp reference/Ravel-master/ravel/tests/data/Rates_All.expected.json \
      /tmp/notbbg-ravel/
   printf '{"instrument":"BTCUSDT","exchange":"binance","price":123.45}\n' \
      > /tmp/notbbg-tsbase/sample.jsonl
   ```
3. Edit `server/configs/dev.yaml` — under `feeds:` flip each block to
   `enabled: true` and set `path:` to the directory you just created.
4. Restart the server. Expected LOG lines:
   ```
   registered feed adapter name=sibelius
   registered feed adapter name=ravel
   registered feed adapter name=tsbase_files
   ```
5. In the **TUI → MON panel** or the desktop status tab, the three
   new adapters must appear with state=connected and BytesReceived
   climbing after the initial scan.
6. Subscribe from a quick curl to verify bus traffic:
   ```bash
   TOKEN=$(cat /tmp/notbbg-desktop.token)
   curl -N "http://localhost:9474/api/v1/subscribe?patterns=sibelius.*,ravel.*,tsbase.*&token=$TOKEN"
   ```
   You should see SSE events within 10–30s (depending on poll
   interval).

### D′. Desktop progressive OHLC (streaming)

Open the desktop app; on the OHLC tab pick an instrument. Expected:

- **Fresh connect**: on first view of an (instrument, timeframe)
  pair with < 50 live candles, the chart auto-triggers a 24h
  backfill. The "Load 24h" button in the header turns amber and
  shows "⟳ N" where N grows as NDJSON chunks arrive. Candles
  populate the chart progressively; the UI remains responsive.
- **Manual reload**: clicking "Load 24h" anytime re-runs the
  stream. During the fetch the button is disabled + amber.
- **Switching instrument**: each new (instrument, tf) pair
  triggers one auto-backfill (the autoBackfilledRef set
  deduplicates).
- **TopBar message counter**: "N msgs" ticks up with SSE
  traffic, caps at "50000+ msgs" (mirrors TUI).

Fail modes:
- Spinner never appears: check `/tmp/notbbg-desktop.token`
  exists and is non-empty; check that the URL the desktop
  opened includes `?token=...`.
- 503 from the server: datalake is disabled — `dev.yaml` needs
  `datalake.enabled: true`.

### E. TUI progressive OHLC (H key)

In the TUI on the OHLC panel:

- Press `H` — status bar flashes "Loading history (24h)…", the
  top-right of the chart area shows "⟳ loading N" where N is
  the chunk counter.
- Panel stays fully interactive during the load ([/] switches
  instrument, -/+ switches timeframe, / opens search).
- On completion the indicator disappears; the candle buffer
  has the last 24h merged in (de-duped by timestamp).

Fail modes:
- No `⟳ loading`: check `/tmp/notbbg-tui.token` exists.
- Error flashes in status bar: inspect the error — usually
  reveals the HTTP status or transport failure.

### D. GetDataRange over HTTP

The new `/api/v1/datarange` streams NDJSON chunks, with `"EOF":true`
on the final line.

```bash
TOKEN=$(cat /tmp/notbbg-desktop.token)
FROM=$(date -u -v-1d +%Y-%m-%dT%H:%M:%SZ)   # macOS; GNU: date -u -d '1 day ago' ...
TO=$(date -u +%Y-%m-%dT%H:%M:%SZ)
curl -sN "http://localhost:9474/api/v1/datarange?topic=ohlc.binance.BTCUSDT&from=$FROM&to=$TO&token=$TOKEN"
```

Pass:
- Response has `Content-Type: application/x-ndjson`.
- Multiple JSON lines stream in (one per chunk, default 500 records
  per chunk).
- Last line has `"EOF":true` and `"CorrelationID":""` (or whatever
  you passed).
- Closing the TCP connection mid-stream cancels the server's scan
  (verify: no goroutine leak in subsequent `datasoak` run).

Fail:
- 503 `{"error":"datarange not configured"}` → datalake is disabled
  or has no path; edit `dev.yaml`.
- 400 `bad from` → your time isn't RFC3339 — use the examples above.

---

## Manual tests for 2026-04-25 work

### Per-tab help (`?`)

1. Launch TUI; press `h` — global helpText overlay appears.
2. Switch to OHLC tab; press `?` — overlay shows OHLC-specific
   keys (`/` filter, `[` `]` instrument nav, `-` `+` timeframe,
   `H` history).
3. Switch to a plugin tab; press `?` — overlay shows the
   plugin's screen ID, topic, input/output cell counts, and
   the selection/Tab-autocomplete hint.
4. Press Esc — overlay closes.

### TUI OHLC sidebar filter

1. OHLC tab, press `/`, type `btc`, Enter — sidebar narrows to
   instruments containing "btc"; status line shows
   `OHLC filter: btc`.
2. Press Esc — filter clears.

### Plugin CLI: install / remove / logs

1. `(cd examples/plugins/hello-world && GOWORK=off go build)`
2. `bin/notbbg plugin install examples/plugins/hello-world`
3. `bin/notbbg plugin list` — shows hello-world.
4. `bin/notbbg plugin logs hello-world -n 50` — last 50 stderr
   lines (server tees plugin stderr to `<dir>/notbbg.log`,
   10 MB rotation, one backup).
5. `bin/notbbg plugin logs hello-world -f` — follow mode.
6. `bin/notbbg plugin remove hello-world` — disappears within 5 s.

### Cancel UI for long plugin jobs

1. Backtester in Historical mode with date range >5 s of work.
2. **TUI:** press `X` on BACKTEST tab — `histResult.status`
   flips to `cancelled` in the cell grid.
3. **Desktop:** click the red `Cancel` button in the panel
   header.
4. **Phone:** tap the red `Cancel running job` pill above the
   cells.
5. Verify: `pgrep bt-engine` returns nothing after cancel.

### MON BACKPRESSURE section

1. Run server + TUI (locally or paired-remote).
2. After ≥10 s the MON tab shows a `BACKPRESSURE` block at the
   top:
   - `bus subs=N topics=N dropped=N <ago>`
   - `wal.cache bus_drop=… wal_drop=… bytes=… segs=… <ago>`
   - `wal.datalake …`
3. Drop counters render red when non-zero.
4. Same section appears on the desktop MON tab.

### Plugin chart / table / image cells

1. Build a plugin that emits all three (use the `pricer`
   example with the new `ChartCell` / `TableCell` / `ImageCell`
   builders in `libs/pluginsdk/cells.go`).
2. **TUI:** charts as 8-row block-character sparklines;
   tables with cyan headers + per-column align; image cells
   as `📎 [IMG <alt>]  <src>` placeholder.
3. **Desktop:** charts as multi-series SVG polylines; tables
   as `<table>` with click-to-sort headers; images as `<img>`
   with `NOTBBG:/abs/path` resolved to `file://`.
4. **Phone:** charts as inline sparklines; tables flat as
   `h1 h2 | v1 v2 / v1 v2`; images as text placeholder.

### dYdX / Drift / Hyperliquid perp + liquidation streams

Configure in `server/configs/dev.yaml`:

```yaml
feeds:
  dex:
    hyperliquid:
      enabled: true
      poll_interval: 5s
    dydx:
      enabled: true
      poll_interval: 5s
      symbols: [BTC-USD, ETH-USD, SOL-USD]   # opt-in to v4 indexer
    drift:
      enabled: true
      poll_interval: 10s
      api_key: drift-v2                       # opt-in to v2 REST poller
```

Then:
1. Start server. LOG tab should show `dydx-v4 connected
   tickers=N`, `hyperliquid connected`, and periodic Drift
   poll completions.
2. Subscribe over SSE:
   `curl -sN http://localhost:9474/api/v1/subscribe?patterns=perp.*.*,liquidation.*.*&token=$TOK`
3. Expect `perp.dydx.<sym>` / `perp.hyperliquid.<sym>` /
   `perp.drift.<sym>-PERP` / `perp.bybit.<sym>` / `perp.okx.<sym>`
   payloads with mark / index / funding / OI fields populated.
4. Liquidations: one bus message per row from Hyperliquid +
   Bybit `allLiquidation` channels, side mapped to taker
   ("buy" = short liquidated, "sell" = long liquidated).

### TUI Settings tab

1. Switch to SETTINGS tab — read-only sections for PATHS /
   SERVER / PANELS / GUI CACHE / WATCHLIST / ALERTS.
2. Verify resolved home matches `--home` flag if set,
   otherwise `$XDG_CONFIG_HOME/notbbg` or `~/.config/notbbg`.

### `--home` flag + XDG

1. `NOTBBG_PASSWORD=… ./bin/notbbg-server --home /tmp/nbbg-home --config server/configs/dev.yaml`
   → uses `/tmp/nbbg-home/plugins/` + `/tmp/nbbg-home/certs/`.
2. Same flag on TUI: `./bin/notbbg --home /tmp/nbbg-home`.
3. Same on collector: `./bin/notbbg-collector --home …`.
4. Without `--home`: `XDG_CONFIG_HOME=/tmp/xdg ./bin/notbbg-server`
   uses `/tmp/xdg/notbbg/`.

### Alert persistence

1. TUI command bar: `/alert set BTCUSDT > 100000`.
2. Stop server, restart it.
3. Alerts list still shows the rule (loaded from
   `<home>/alerts.json`).

### Agent NOTBBG: artifact open

1. AGENT tab — issue a `/agent` query whose response includes
   a line like `plot saved NOTBBG:/tmp/chart.png`.
2. Press `O` (or lowercase `o`) — `/tmp/chart.png` opens via
   `open` (macOS) / `xdg-open` (Linux). Status line shows
   `opened /tmp/chart.png`.

### Selection-cell Tab autocomplete

1. On a plugin tab with an `input_selection` cell (e.g. PRICER
   ticker), Enter to edit, type `bt`, press Tab — value
   extends to the longest common prefix (`BTC`); status line
   shows `3 matches: BTCUSDT BTCUSDC BTCUSD`.

### Backpressure end-to-end (smoke)

1. Run server + datasoak; observe periodic
   `cache writer stats (WAL)` lines in the LOG tab.
2. Stop the BBolt store mid-run (e.g. `chmod -w` the db file)
   — `wal_dropped` counter starts incrementing in the
   BACKPRESSURE MON section once cap is hit.
3. Restore writes; counters stop incrementing; queue drains.

---

## Next-steps reference

Detailed plans for the remaining "deferred-by-environment"
items (bt-engine E2E, schema U2-U8, plugin Phase 8/9/10) live
in `todos/NEXT-STEPS.md`. That doc is the single source of
truth for "what to do next" — read it before starting any of
those items.

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| TUI shows DISCONNECTED | Server not running. `make build` then run TUI |
| "Waiting for data..." | Wait 3-5s for feeds to connect |
| "open cache: timeout" | Kill stale server: `pkill notbbg-server` |
| OHLC shows stale times | Old binary. `make build` and restart |
| No LOB data | Check Binance WS connected (LOG tab) |
| No news | RSS feeds poll every 2m, wait for first cycle |
| DEX data missing | Check LOG tab for DeFi Llama errors |
| AGENT tab slow | Each query spawns `claude -p` (cold start). Normal — takes 5-15s per response |
| OKX/Bybit not connecting | Verify WS endpoints in dev.yaml |
| Datalake empty | Check `datalake.enabled: true` in config |
| Collector won't start | Check TCP port 9473 not in use |
