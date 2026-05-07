<p align="center">
  <img src="image/logo.svg" width="180" alt="notbbg">
</p>

<h1 align="center">notbbg</h1>

<p align="center">
  <a href="#"><img src="https://img.shields.io/badge/version-0.3.0-ff9d2b?style=flat-square" alt="version"></a>
  <a href="#"><img src="https://img.shields.io/badge/build-stable-2ed573?style=flat-square" alt="build"></a>
  <a href="#"><img src="https://img.shields.io/badge/go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white" alt="go"></a>
  <a href="#"><img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows*-2ed573?style=flat-square" alt="platforms"></a>
  <a href="#"><img src="https://img.shields.io/badge/clients-TUI%20%7C%20Desktop%20%7C%20Phone-ff9d2b?style=flat-square" alt="clients"></a>
  <a href="#"><img src="https://img.shields.io/badge/crypto-ML--KEM--768%20%7C%20XChaCha20-6c5ce7?style=flat-square" alt="crypto"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" alt="license"></a>
</p>

<p align="center">
  <sub>* Windows builds, but is not fully tested yet.</sub>
</p>

<p align="center">
  <b>Self-hosted market data terminal for casual traders.</b><br>
  Bloomberg-style TUI, cross-platform desktop GUI, and a read-only phone companion — fed by a single Go server that streams from 17+ sources and backs up to your home machine over post-quantum TLS.
</p>

<p align="center">
  Built for people who want pro-grade market tooling without the monthly terminal fee: runs on a laptop, pairs with a phone, persists everything to a Hive-partitioned datalake.
</p>

---

> **This is the demo / OSS cut.** It's the stable build — boots on a fresh
> clone with no API keys, ships a small set of example plugins, and
> covers the surfaces a casual trader actually uses. A handful of
> additional plugins (OMS, backtester, pricer, vol tools, etc.) may
> ship later — no promises, no timeline.

![TUI OHLC](image/README/tui-ohlc.png)

## What's Inside

### Clients

Every client talks to the same Go server, so panels stay in sync across devices.

| Client | Stack | Transport | Notes |
|--------|-------|-----------|-------|
| **TUI** | Go + Bubbletea + Lipgloss | Unix socket | 10 core panels + plugin tabs, embedded Claude agent, full keyboard |
| **Desktop** | Electron + React 19 + Vite | HTTP / SSE | 1:1 port of TUI logic, TradingView charts |
| **Phone** | React Native + Expo | HTTP / SSE | Read-only, sub-second updates via SSE, QR-pair from TUI |

### Data Feeds

17+ adapters across CEX, DEX, indices, FX, commodities, and news.

| Category | Sources | Protocol |
|----------|---------|----------|
| **CEX** | Binance, OKX, Bybit, Bitget | WebSocket |
| **DEX** | Hyperliquid, Uniswap, GMX, dYdX, Drift, Serum, Raydium, Jupiter | WS + DeFi Llama |
| **Indices** | S&P 500, DJIA, NASDAQ, FTSE, DAX, CAC 40, Nikkei, KOSPI, Russell 2000 | Yahoo Finance |
| **FX** | EUR/USD, GBP/USD, USD/JPY, USD/KRW | Yahoo Finance |
| **Commodities** | Gold, Silver, Crude Oil, Natural Gas, US Dollar Index | Yahoo Finance |
| **Crypto** | CoinGecko (50+ tokens), Fear & Greed, Mempool.space | REST |
| **News** | CoinTelegraph, CoinDesk, The Block, Bloomberg, CNBC, FT, Wired, X.com | RSS (20+ feeds) |

### Core Systems

- **Message bus** — in-process pub/sub, one topic per instrument × type
- **Credit-based backpressure** — Erlang GenStage-inspired demand signalling, no drops under load
- **BBolt cache + BM25 search** — on-disk cache of OHLC/news with full-text search
- **Cron scheduler** — periodic REST pulls for non-streaming feeds
- **HTTP/SSE gateway** — token-auth'd snapshot + stream endpoints for desktop/phone
- **Datalake writer** — Hive-partitioned JSONL, local or remote

### Security

- **Transport** — TLS 1.3 + ML-KEM-768 post-quantum key exchange (Cloudflare circl)
- **At rest** — XChaCha20-Poly1305 + Argon2id for config and token encryption
- **Auth** — one-time pairing tokens (10min TTL), session tokens (30-day), separate phone token
- **Phone** — read-only session, no writes, no agent, no config changes

### Remote Collector

Push every tick, candle, and headline to a backup machine over an encrypted relay.

```
Local                                    Remote (home server)
├── server ──TLS 1.3 + ML-KEM-768──►    collector
│                                        └── datalake/type=ohlc/exchange=binance/
│                                               instrument=BTCUSDT/year=2026/...
└── tui / desktop / phone
```

### Embedded AI Agent

- Claude terminal inside the TUI (AGENT panel)
- HTTP agent API in the desktop app
- Skills defined in [SKILLS.md](SKILLS.md) — query feeds, explain candles, search news

## Requirements

Install these before running anything — the Quick Start commands will fail without them.

- **Go 1.25+** — https://go.dev/dl/ or install with your package manager
- **Node 20+** — https://nodejs.org/en/download or install with your package manager
- **make** — https://gnuwin32.sourceforge.net/packages/make.htm (windows only, already installed on macOS/Linux)
- **Expo CLI + EAS** — https://docs.expo.dev/get-started/installation/ (only needed for phone app builds)
- **A remote machine with an open port** — collector backup only (optional)

Verify before continuing: `go version` (need 1.25+) and `node --version` (need v20+)

## Quick Start

Fresh-clone first-launch — no API keys, six no-auth CEX venues
(binance / okx / bybit / bitget / gate / mexc / htx), three majors
(BTC / ETH / SOL), 7d backfill, the demo plugin set:

```bash
make build
make oss-bootstrap    # plugins-oss + run-server-oss back-to-back
```

Or break it into the two steps:

```bash
make build
make plugins-oss      # deploy hello-world + formula-demo + monitor + ohlc-png + formulettes-runner
make run-server-oss   # uses server/configs/dev-oss.yaml
```

In another terminal, pick a client:

```bash
make run-tui          # bubbletea TUI
make run-desktop      # Electron + React
make run-phone        # Expo dev server (press 'a'/'i' for Android/iOS)

# Health probe (server must be up).
make smoke
```

The OSS profile lives at `scripts/plugin-profiles/oss.txt`; the
matching server config lives at `server/configs/dev-oss.yaml`.

### Phone pairing

The phone reads its session token from the server. With the
server up, run `cat /tmp/notbbg-phone.token` and paste the
output into Settings → TOKEN inside the Expo app. (Or scan the
QR code from `http://localhost:9474/api/v1/pair/qr`.)

On a fresh install the phone redirects to Settings on launch
until a token is stored. After pairing it lands on Watchlist
with a "LIVE — last update Ns ago" indicator showing real-time
freshness.

### Common gotchas

#### "Password for collector localhost:9473:" prompt on TUI launch

The TUI is asking for the **vault password** — the one you used
with `notbbg-collector -init-secrets` (or
`notbbg-server -init-secrets`). It's needed to decrypt a
previously-paired collector token stored in
`~/.config/notbbg/config.yaml`.

The collector is **optional** — it's the remote-backup component
that mirrors the bus to a remote machine's datalake over PQC TLS.
Most users running everything locally don't need it.

Three ways through the prompt:

1. **Type the vault password** (whatever you set with
   `-init-secrets`). The TUI then connects to both the local
   server and pushes a copy to the collector.
2. **Bypass via env var:** `NOTBBG_PASSWORD='<pwd>' make run-tui`.
3. **Drop the collector pairing** (clean slate, no remote
   backup):
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
   The TUI now skips the prompt and connects only to the local
   server. Re-pair later with
   `./bin/notbbg pair-collector <host>:9473 <token>`.

#### "Is the collector running?" — checking

```bash
# port test
nc -z localhost 9473 && echo open || echo closed
# process test
pgrep -fl notbbg-collector
```

If you actually want it running:
```bash
# one-time vault init (asks for a password to remember)
./bin/notbbg-collector -init-secrets -enc-config /tmp/collector-secrets.enc

# generate + start (token + password env vars)
TOKEN=$(./bin/notbbg-collector -config server/configs/collector-local.yaml -pair 2>/dev/null \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
NOTBBG_TOKEN=$TOKEN NOTBBG_PASSWORD='<vault-pwd>' \
  ./bin/notbbg-collector -config server/configs/collector-local.yaml \
    -enc-config /tmp/collector-secrets.enc &
```

#### TLS handshake fails with `make smoke` on macOS

The macOS system curl ships with LibreSSL/3.3.6 which can't
negotiate against the server's self-signed TLS 1.3 cert. The
target falls back to `/opt/homebrew/opt/curl/bin/curl` (OpenSSL)
and finally a bare `nc -z` port check, so you'll always get a
useful answer. The actual GUIs (Electron Chromium, react-native-sse,
TUI's own Go TLS stack) handle the handshake fine.

## Screenshots

### TUI

| Candlestick (OHLC) | Order Book (LOB) | News Feed |
|---|---|---|
| ![](image/README/tui-ohlc.png) | ![](image/README/tui-lob.png) | ![](image/README/tui-news.png) |
| **Article Detail** | **Feed Monitor** | **Phone Pairing** |
| ![](image/README/tui-news-2.png) | ![](image/README/tui-mon.png) | ![](image/README/tui-pair.png) |

| Trades Tape | Sanity / Consistency | OHLC Backfill |
|---|---|---|
| ![](image/README/tui-trades.png) | ![](image/README/tui-sanity.png) | ![](image/README/tui-ohlc-backfill.png) |
| **PLOT Image Cells** | **Formulettes (FORM)** | **Scroll Indicator** |
| ![](image/README/tui-plot.png) | ![](image/README/tui-form.png) | ![](image/README/tui-scroll.png) |

### Desktop (Electron)

| OHLC | LOB | News | Pairing |
|---|---|---|---|
| ![](image/README/desktop-ohlc.png) | ![](image/README/desktop-lob.png) | ![](image/README/desktop-news.png) | ![](image/README/desktop-pair.png) |

| Trades Tape | Sanity / Consistency | Formulettes (FORM) | OHLC Streaming History |
|---|---|---|---|
| ![](image/README/desktop-trades.png) | ![](image/README/desktop-sanity.png) | ![](image/README/desktop-form.png) | ![](image/README/desktop-ohlc-history.png) |

### Phone (React Native)

| Watchlist | LOB | News (BM25) | Settings |
|---|---|---|---|
| ![](image/README/phone/watchlist.png) | ![](image/README/phone/lob.png) | ![](image/README/phone/news.png) | ![](image/README/phone/settings.png) |

| Trades (modal picker) | Trades — Picker Open | OHLC | Sanity |
|---|---|---|---|
| ![](image/README/phone/trades.png) | ![](image/README/phone/trades-picker.png) | ![](image/README/phone/ohlc.png) | ![](image/README/phone/sanity.png) |

## Architecture

```
Server (Go)                          Collector (remote)
├── 17 feed adapters (WS/REST)  ──TLS+PQC──►  Datalake writer
├── Message bus (pub/sub)                      (Hive-partitioned JSONL)
├── BBolt cache + BM25 search
├── Credit-based backpressure
├── Cron scheduler
├── HTTP/SSE gateway ──────────►  Desktop (Electron/React)
│   └── /api/v1/snapshot?mode=latest ►  Phone (React Native, polling)
└── Unix socket ───────────────►  TUI (Bubbletea)
```

## Keyboard Shortcuts (TUI)

A few bindings shifted in this cut — the panel list grew (TRADES,
SANITY, SETTINGS were added) and digit keys are now reserved for
plugin cell input, so panel-jump moved to `Alt+digit`.

| Key | Action |
|-----|--------|
| `Alt+1`–`Alt+9` | Jump to panel (OHLC, LOB, TRADES, NEWS, ALERTS, SANITY, MON, LOG, AGENT) |
| `TAB` / `Shift+TAB` | Cycle panels (incl. SETTINGS and any loaded plugin tabs) |
| `[` / `]` or `←` / `→` | Previous / next instrument (OHLC / LOB / TRADES) |
| `-` / `+` | Previous / next timeframe (OHLC) |
| `H` | Load 24h history via DataRange (OHLC, non-blocking) |
| `j` / `k` or `↓` / `↑` | Step scroll (NEWS, SANITY, MON, LOG, SETTINGS, AGENT, overlays) |
| `PgDn` / `PgUp` or `Ctrl+F` / `Ctrl+B` | Page-step scroll |
| `g` / `G` | Jump to top / bottom of scrollable panel |
| `Enter` | Read article / send agent input / commit cell edit |
| `/` or `:` | Enter command mode (search / filter / `/BTC`, `/LOB`, ...) |
| `o` / `y` | Open image cell in viewer / copy path (PLOT) |
| `X` | Cancel running plugin job on the active plugin tab |
| `h` | Global help overlay |
| `?` | Per-tab help overlay |
| `ESC` | Close overlay / clear filter / cancel command |
| `q` | Quit |

## CLI Commands

```bash
notbbg                              # Launch TUI
notbbg export ohlc BTCUSDT -f csv   # Export OHLC data
notbbg news search BTC              # Search news by keyword
notbbg feeds list                   # Show feed statuses
notbbg history BTCUSDT              # Query cached data
notbbg agent list                   # List agent skills
notbbg pair-collector host:9473 tok # Pair with remote collector
notbbg pair-collector --forget      # Remove pairing
```

## Build & Test

| Command | What it does |
|---------|-------------|
| `make build` | Server + collector + TUI |
| `make test` | All Go tests (race detector) |
| `make check` | TypeScript checks (phone + desktop) |
| `make dist` | Cross-platform: darwin-arm64, linux-amd64, linux-arm64, windows-amd64 (Windows not fully tested yet) |
| `make phone-dev` | Expo dev server |
| `make phone-apk` | Build APK via EAS |
| `make desktop-dev` | Vite dev server |

## Remote Collector

```bash
# Remote machine:
./bin/notbbg-collector -init-secrets -enc-config configs/secrets.enc
./bin/notbbg-collector -pair                    # print pairing token
NOTBBG_TOKEN=<tok> ./bin/notbbg-collector ...   # start

# Local machine:
./bin/notbbg pair-collector ajax:9473 <token>   # one-time pairing
./bin/notbbg                                    # auto-pushes to collector
```

Data persisted as Hive-partitioned JSONL:

```
datalake/type=ohlc/exchange=binance/instrument=BTCUSDT/year=2026/month=03/day=30/data.jsonl
```

## Maybe Later

This OSS cut ships a small set of demo plugins (`hello-world`,
`formula-demo`, `monitor`, `ohlc-png`, `formulettes-runner`). The
plugin SDK is here and the loader works against any binary that
speaks the protocol — additional plugins (OMS, backtester, pricer,
optimizer, vol tools, portfolio, swaption, timeseries) live in the
internal tree and *may* land in a future release. No promises, no
timeline.

Other things on the wishlist: more CEX/DEX adapters, Windows
hardening, macOS app signing, Android APK distribution, Gobra/TLA+
proofs for the backpressure protocol. 

## Project Structure

```
server/           Go server — feeds, bus, cache, auth, transport, datalake, cron
tui/              Go TUI — bubbletea panels, agent terminal, CLI commands
desktop/          Electron + React desktop app
phone/            React Native + Expo phone app (read-only)
formal/           TLA+ specifications (backpressure protocol)
scripts/          local-test.sh, local-test-desktop.sh

```

## Dependencies

| Component | Technology | License |
|-----------|-----------|---------|
| Server | Go 1.25 | — |
| TUI | Bubbletea + Lipgloss | MIT |
| Desktop | Electron + React 19 + Vite | MIT |
| Phone | React Native + Expo | MIT |
| Charts | lightweight-charts (TradingView) | Apache 2.0 |
| PQC | Cloudflare circl (ML-KEM-768) | BSD-3 |
| Cache | BBolt (etcd) | MIT |
| Crypto | XChaCha20-Poly1305, Argon2id (golang.org/x/crypto) | BSD-3 |
| RSS | gofeed | MIT |
| WebSocket | gorilla/websocket | BSD-2 |

## License

Apache License 2.0 — see [LICENSE](LICENSE) for the full text.
