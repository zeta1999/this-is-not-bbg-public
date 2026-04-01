# notbbg — Market Data Terminal for Casual Traders

Real-time market data terminal with TUI, desktop GUI (Electron), and an experimental phone companion app (React Native). Streams live data from 17+ sources across CEX, DEX, indices, FX, commodities, and news — with post-quantum encrypted remote backup.

> **v0.2.0** — actively developed. Phone app is experimental.

![TUI OHLC](image/README/tui-ohlc.png)

## Features

- **7 panels**: OHLC charts, order book (LOB), news feed, alerts, feed monitor, server log, AI agent
- **17 feed adapters**: Binance, OKX, Bybit, Bitget, Hyperliquid, CoinGecko, Yahoo Finance, 20+ RSS feeds
- **3 clients**: TUI (terminal), Desktop (Electron), Phone (React Native, experimental)
- **Phone pairing**: generate tokens from TUI (`/PAIR`) or desktop app, QR code or manual paste
- **Per-instrument navigation**: `[`/`]` to cycle, `/` to search, `-`/`+` for timeframes
- **Credit-based backpressure**: Erlang GenStage-inspired flow control prevents data loss
- **Remote collector**: push data to a backup machine over TLS 1.3 + ML-KEM-768 (post-quantum)
- **Datalake**: Hive-partitioned JSONL export of all market data
- **AI agent**: embedded Claude terminal in TUI, HTTP API in desktop app
- **Security**: token auth, Argon2id encrypted secrets, PQC key exchange

## Screenshots

### TUI — Candlestick Charts (OHLC)

![TUI OHLC](image/README/tui-ohlc.png)

### TUI — Order Book (LOB)

![TUI LOB](image/README/tui-lob.png)

### TUI — News Feed

![TUI News](image/README/tui-news.png)

### TUI — News Article Detail

![TUI News Detail](image/README/tui-news-2.png)

### TUI — Feed Monitor

![TUI Monitor](image/README/tui-mon.png)

### TUI — Phone Pairing (`/PAIR` command)

![TUI Pair](image/README/tui-pair.png)

### Desktop App (Electron) — OHLC

![Desktop OHLC](image/README/desktop-ohlc.png)

### Desktop App — LOB

![Desktop LOB](image/README/desktop-lob.png)

### Desktop App — News

![Desktop News](image/README/desktop-news.png)

### Desktop App — Phone Pairing Modal

![Desktop Pair](image/README/desktop-pair.png)

### Phone App — Watchlist (experimental)

![Phone Watchlist](image/README/phone/watchlist.png)

### Phone App — Order Book

![Phone LOB](image/README/phone/lob.png)

### Phone App — News (BM25 search)

![Phone News](image/README/phone/news.png)

### Phone App — Settings

![Phone Settings](image/README/phone/settings.png)

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
make phone-install    # Install dependencies
make phone-dev        # Start Expo dev server (press 'a' for Android, 'i' for iOS)
```

Pair the phone: TUI `/PAIR` command, desktop phone button, or `cat /tmp/notbbg-phone.token`.
The phone app is **read-only** — no writes, no agent, no config changes.
See [PHONE-TESTING.md](PHONE-TESTING.md) for full pairing and testing guide.

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

## Data Sources

| Category              | Sources                                                               | Protocol        |
| --------------------- | --------------------------------------------------------------------- | --------------- |
| **CEX**         | Binance, OKX, Bybit, Bitget                                           | WebSocket       |
| **DEX**         | Hyperliquid, Uniswap, GMX, dYdX, Drift, Serum, Raydium, Jupiter       | WS + DeFi Llama |
| **Indices**     | S&P 500, DJIA, NASDAQ, FTSE, DAX, CAC 40, Nikkei, KOSPI, Russell 2000 | Yahoo Finance   |
| **FX**          | EUR/USD, GBP/USD, USD/JPY, USD/KRW                                    | Yahoo Finance   |
| **Commodities** | Gold, Silver, Crude Oil, Natural Gas, US Dollar Index                 | Yahoo Finance   |
| **Crypto**      | CoinGecko (50+ tokens), Fear & Greed, Mempool.space                   | REST            |
| **News**        | CoinTelegraph, CoinDesk, The Block, Bloomberg, CNBC, FT, Wired, X.com | RSS             |

## Keyboard Shortcuts (TUI)

| Key                              | Action                                                   |
| -------------------------------- | -------------------------------------------------------- |
| `1`-`7`                      | Jump to panel (OHLC, LOB, NEWS, ALERTS, MON, LOG, AGENT) |
| `TAB` / `Shift+TAB`          | Cycle panels                                             |
| `[` / `]` or `←` / `→` | Previous / next instrument                               |
| `-` / `+`                    | Previous / next timeframe                                |
| `j` / `k`                    | Navigate news headlines                                  |
| `Enter`                        | Read article / send agent input                          |
| `/`                            | Search instruments or filter news                        |
| `h` or `?`                   | Help overlay                                             |
| `q`                            | Quit                                                     |

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

```bash
make build            # Server + collector + TUI
make test             # All Go tests (race detector)
make check            # TypeScript checks (phone + desktop)
make dist             # Cross-platform: darwin-arm64, linux-amd64, linux-arm64, windows-amd64
make phone-dev        # Expo dev server
make phone-apk        # Build APK via EAS
make desktop-dev      # Vite dev server
```

## Security

| Layer               | Protection                                                        |
| ------------------- | ----------------------------------------------------------------- |
| **Transport** | TLS 1.3 + ML-KEM-768 post-quantum key exchange                    |
| **Auth**      | One-time pairing tokens (10min TTL), session tokens (30 days)     |
| **At Rest**   | XChaCha20-Poly1305 + Argon2id for config and token encryption     |
| **HTTP**      | Token auth on all data endpoints, CORS, localhost-only by default |
| **Phone**     | Separate session token, read-only access, no writes               |

See [SECURITY.md](SECURITY.md) for the full threat model.

## Remote Collector

Back up all market data to a remote machine:

```bash
# Remote machine:
./bin/notbbg-collector -init-secrets -enc-config configs/secrets.enc
./bin/notbbg-collector -pair                    # get token
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

Current version is **v0.2.0**. Planned for future releases:

- **Plugin system**: C/C++ native plugins for custom analytics, loaded via shared libraries
  - Order Management System (OMS) integration
  - Time series tools: market regime detection, synthetic series generation, cointegration analysis
  - Accelerated backtesting engine (vectorized, SIMD-optimized)
  - Custom indicator pipelines (Rust/C++ with Go FFI bridge)
- **More data sources**: OKX/Bitget perpetuals, Gate.io, MEXC, topic-specific RSS (Solana, semiconductors, macro)
- **News improvements**: sort by freshness, configurable retention, RSS error monitoring
- **Formal verification**: Gobra proofs for relay invariants, TLA+ model checking
- **Platform**: Windows support, macOS app signing, Android APK distribution

See [SPEC.md](SPEC.md) for the full backlog.

## BSOM (Bill of Materials)

| Component | Technology                                         | License    |
| --------- | -------------------------------------------------- | ---------- |
| Server    | Go 1.25                                            | —         |
| TUI       | Go + Bubbletea + Lipgloss                          | MIT        |
| Desktop   | Electron + React 19 + Vite                         | MIT        |
| Phone     | React Native + Expo (experimental)                 | MIT        |
| Charts    | lightweight-charts (TradingView)                   | Apache 2.0 |
| PQC       | Cloudflare circl (ML-KEM-768)                      | BSD-3      |
| Cache     | BBolt (etcd)                                       | MIT        |
| Crypto    | XChaCha20-Poly1305, Argon2id (golang.org/x/crypto) | BSD-3      |
| RSS       | gofeed                                             | MIT        |
| WebSocket | gorilla/websocket                                  | BSD-2      |

## Project Structure

```
server/           Go server — feeds, bus, cache, auth, transport, datalake, cron
tui/              Go TUI — bubbletea panels, agent terminal, CLI commands
desktop/          Electron + React desktop app
phone/            React Native + Expo phone app (experimental, read-only)
formal/           TLA+ specifications (backpressure protocol)
scripts/          local-test.sh, local-test-desktop.sh
```
