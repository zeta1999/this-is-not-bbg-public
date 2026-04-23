<p align="center">
  <img src="image/logo.svg" width="180" alt="notbbg">
</p>

<h1 align="center">notbbg</h1>

<p align="center">
  <a href="#"><img src="https://img.shields.io/badge/version-0.2.0-ff9d2b?style=flat-square" alt="version"></a>
  <a href="#"><img src="https://img.shields.io/badge/go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white" alt="go"></a>
  <a href="#"><img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows*-2ed573?style=flat-square" alt="platforms"></a>
  <a href="#"><img src="https://img.shields.io/badge/clients-TUI%20%7C%20Desktop%20%7C%20Phone-ff9d2b?style=flat-square" alt="clients"></a>
  <a href="#"><img src="https://img.shields.io/badge/crypto-ML--KEM--768%20%7C%20XChaCha20-6c5ce7?style=flat-square" alt="crypto"></a>
  <a href="#"><img src="https://img.shields.io/badge/license-see%20LICENSE-lightgrey?style=flat-square" alt="license"></a>
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

> **v0.2.0** — actively developed. Phone app is experimental. See [SPEC.md](SPEC.md) for the roadmap.

> ⚠️ **Interim update — 2026-04-23.** `main` is ahead of the last
> publicly-pushed commit by several in-flight tracks (adapter
> wiring for Sibelius/Ravel/tsbase-files, HTTP `GetDataRange`
> streaming, TUI progressive history, phone TRADES ANR fix,
> Uniswap verification). These haven't gone through full manual
> end-to-end testing across all three GUIs yet. If you need a
> build that "just runs", check out the last published push at
> commit [`289139c`](../../commit/289139c) (or `git checkout
> 289139c`) — that's the last manually-verified release point.
> See [STATUS.md](STATUS.md) for what's new on `HEAD` and
> [TESTING.md](TESTING.md) §"What should work after 2026-04-23
> tracks" for the manual-test procedure.

![TUI OHLC](image/README/tui-ohlc.png)

## What's Inside

### Clients

Every client talks to the same Go server, so panels stay in sync across devices.

| Client | Stack | Transport | Notes |
|--------|-------|-----------|-------|
| **TUI** | Go + Bubbletea + Lipgloss | Unix socket | 7 panels, embedded Claude agent, full keyboard |
| **Desktop** | Electron + React 19 + Vite | HTTP / SSE | 1:1 port of TUI logic, TradingView charts |
| **Phone** | React Native + Expo | HTTP polling | Experimental, read-only, QR-pair from TUI |

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

## Quick Start

```bash
# Build everything (server + TUI + collector)
make build

# Run TUI (auto-starts server)
./bin/notbbg

# Run with desktop GUI
./scripts/local-test-desktop.sh

# Run with remote collector backup
./scripts/local-test.sh
```

### Phone App (experimental)

```bash
make phone-install    # Install deps
make phone-dev        # Start Expo dev server (press 'a' for Android, 'i' for iOS)
```

Pair the phone via TUI `/PAIR`, the desktop pairing modal, or `cat /tmp/notbbg-phone.token`. See [PHONE-TESTING.md](PHONE-TESTING.md).

## Screenshots

### TUI

| Candlestick (OHLC) | Order Book (LOB) | News Feed |
|---|---|---|
| ![](image/README/tui-ohlc.png) | ![](image/README/tui-lob.png) | ![](image/README/tui-news.png) |
| **Article Detail** | **Feed Monitor** | **Phone Pairing** |
| ![](image/README/tui-news-2.png) | ![](image/README/tui-mon.png) | ![](image/README/tui-pair.png) |

### Desktop (Electron)

| OHLC | LOB | News | Pairing |
|---|---|---|---|
| ![](image/README/desktop-ohlc.png) | ![](image/README/desktop-lob.png) | ![](image/README/desktop-news.png) | ![](image/README/desktop-pair.png) |

### Phone (React Native, experimental)

| Watchlist | LOB | News (BM25) | Settings |
|---|---|---|---|
| ![](image/README/phone/watchlist.png) | ![](image/README/phone/lob.png) | ![](image/README/phone/news.png) | ![](image/README/phone/settings.png) |

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

| Key | Action |
|-----|--------|
| `1`–`7` | Jump to panel (OHLC, LOB, NEWS, ALERTS, MON, LOG, AGENT) |
| `TAB` / `Shift+TAB` | Cycle panels |
| `[` / `]` or `←` / `→` | Previous / next instrument |
| `-` / `+` | Previous / next timeframe |
| `j` / `k` | Navigate news headlines |
| `Enter` | Read article / send agent input |
| `/` | Search instruments or filter news |
| `h` or `?` | Help overlay |
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

## Roadmap

Planned for future releases (current version is **v0.2.0**):

- **Plugin system** — C/C++ native plugins loaded via shared libs (OMS, backtesting, regime detection, SIMD indicators)
- **More feeds** — OKX/Bitget perpetuals, Gate.io, MEXC, topic-specific RSS (Solana, semiconductors, macro)
- **News polish** — sort by freshness, configurable retention, RSS error monitoring
- **Formal verification** — Gobra proofs for relay invariants, TLA+ model checking
- **Platform** — Windows support (builds today, not fully tested yet), macOS app signing, Android APK distribution

See [SPEC.md](SPEC.md) for the full backlog.

## Project Structure

```
server/           Go server — feeds, bus, cache, auth, transport, datalake, cron
tui/              Go TUI — bubbletea panels, agent terminal, CLI commands
desktop/          Electron + React desktop app
phone/            React Native + Expo phone app (experimental, read-only)
formal/           TLA+ specifications (backpressure protocol)
scripts/          local-test.sh, local-test-desktop.sh
docs/             Data update playbook, per-dataset audits, protocol notes
```

## Dependencies (BSOM)

| Component | Technology | License |
|-----------|-----------|---------|
| Server | Go 1.25 | — |
| TUI | Bubbletea + Lipgloss | MIT |
| Desktop | Electron + React 19 + Vite | MIT |
| Phone | React Native + Expo (experimental) | MIT |
| Charts | lightweight-charts (TradingView) | Apache 2.0 |
| PQC | Cloudflare circl (ML-KEM-768) | BSD-3 |
| Cache | BBolt (etcd) | MIT |
| Crypto | XChaCha20-Poly1305, Argon2id (golang.org/x/crypto) | BSD-3 |
| RSS | gofeed | MIT |
| WebSocket | gorilla/websocket | BSD-2 |

## Requirements

- Go 1.25+
- Node 20+ (for desktop and phone)
- Expo CLI + EAS (for phone APK builds)
- A remote machine with a reachable port (optional, for collector backup)

## Documentation

- [PHONE-TESTING.md](PHONE-TESTING.md) — Phone pairing, testing, APK builds
- [TESTING.md](TESTING.md) — Manual testing guide (all components)
- [SECURITY.md](SECURITY.md) — Security model, pairing flow, threat analysis
- [PROTOCOLS.md](PROTOCOLS.md) — Wire protocol, backpressure, PQC handshake
- [SKILLS.md](SKILLS.md) — Agent skills and TUI keyboard reference
- [SPEC.md](SPEC.md) — Roadmap and open TODOs

## License

See [LICENSE](LICENSE) for details.
