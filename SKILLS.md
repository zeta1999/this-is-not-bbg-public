# SKILLS.md — Agent Skill Definitions for notbbg

Agents connect to the notbbg server as standard clients (via Unix socket or TCP)
and subscribe to data topics. They process incoming data and publish suggestions
to the `agent.suggestion` topic.

## TUI Keyboard Reference

### Global
| Key | Action |
|---|---|
| `TAB` / `Shift+TAB` | Next / previous panel |
| `1`-`6` | Jump to panel (OHLC, LOB, NEWS, ALERTS, MON, LOG) |
| `/` or `:` | Enter command mode |
| `h` or `?` | Show help overlay |
| `ESC` | Close overlay / cancel command |
| `q` | Quit (kills managed server) |

### OHLC Panel
| Key | Action |
|---|---|
| `[` / `]` or `←` / `→` | Previous / next instrument |
| `-` / `+` | Previous / next timeframe (1m, 5m, 1h, 1d) |

### Commands (type `/` then command)
| Command | Action |
|---|---|
| `BTC`, `ETH`, `SOL`... | Jump to OHLC for that instrument |
| `LOB` | Order book panel |
| `NEWS` | News feed |
| `ALERTS` | Alerts panel |
| `MON` | Feed monitor |
| `LOG` | Server log viewer |
| `ALERT SET <SYM> > <PRICE>` | Price alert (e.g. `ALERT SET BTCUSDT > 100000`) |
| `ALERT SET KEYWORD <word>` | Keyword alert on news |
| `PAIR` | QR code for phone app pairing |
| `HELP` | Show help overlay |

## Panels

1. **OHLC** — Candlestick chart with per-instrument/per-timeframe data. Right sidebar lists known instruments with last price and update age.
2. **LOB** — Level-2 order book (bids/asks).
3. **NEWS** — Market news feed with timestamps and source.
4. **ALERTS** — Alert management (price above/below, keyword).
5. **MON** — Feed health monitor (connection status, latency, errors).
6. **LOG** — Server log viewer (color-coded INFO/WARN/ERROR, last 500 lines).

## Agent Skills

### news-scan
**Description**: Scan incoming news items for relevance to the user's watchlist and portfolio.
**Input topics**: `news`
**Output topic**: `agent.suggestion`
**Behavior**:
- Subscribe to the `news` topic
- For each incoming news item, evaluate relevance against the user's watchlist symbols
- Score relevance based on: ticker mention (high), sector match (medium), keyword overlap (low)
- If relevance score > threshold, publish a suggestion with:
  - `type: "news_highlight"`
  - `title`: the news headline
  - `reason`: why it's relevant (e.g., "Mentions BTC which is in your watchlist")
  - `urgency`: "high" for price-moving events, "medium" for sector news, "low" for general

### anomaly-detect
**Description**: Monitor price and volume data for unusual patterns.
**Input topics**: `ohlc.*.*`, `trade.*.*`
**Output topic**: `agent.suggestion`
**Behavior**:
- Track rolling statistics (mean, stddev) for price changes and volume per instrument
- Detect anomalies: price move > 2 stddev, volume > 3x rolling average, unusual spread widening
- On anomaly detection, publish a suggestion with:
  - `type: "anomaly"`
  - `instrument`: the affected instrument
  - `metric`: what's anomalous (e.g., "price_change", "volume_spike", "spread")
  - `value`: the observed value
  - `expected`: the expected range
  - `severity`: "critical" (>4 stddev), "warning" (>2 stddev), "info" (>1.5 stddev)

### summarize
**Description**: Generate a summary of recent market activity for a given ticker or the whole portfolio.
**Input topics**: `ohlc.*.*`, `news`, `indicator.*`
**Output topic**: `agent.suggestion`
**Behavior**:
- When invoked (via command `AGENT SUMMARIZE [instrument]`), gather:
  - Last 24h OHLC data for the instrument(s)
  - Related news items (via BM25 search)
  - Relevant indicators (Fear & Greed, BTC fees, macro data)
- Compose a structured summary:
  - Price action: open, high, low, close, change %
  - Key news: top 3 relevant headlines
  - Indicators: current values and trends
  - Publish as suggestion with `type: "summary"`

### correlation-watch
**Description**: Monitor cross-asset correlations and alert on divergence.
**Input topics**: `ohlc.*.*`, `indicator.*`
**Output topic**: `agent.suggestion`
**Behavior**:
- Track rolling correlations between configured pairs (e.g., BTC/SPX, ETH/BTC, Gold/DXY)
- Alert when correlation breaks down (e.g., BTC and SPX usually correlated but diverging)
- Publish suggestion with:
  - `type: "correlation_break"`
  - `pair`: the two instruments
  - `expected_correlation`: rolling 30d correlation
  - `current`: current short-term correlation
  - `direction`: which way each is moving

## Protocol

Agents communicate using the standard notbbg protobuf wire format:
- Connect via Unix socket or TCP+TLS
- Authenticate with a session token (CLIENT_TYPE_AGENT)
- Send SubscribeRequest for input topics
- Receive stream of Update messages
- Publish suggestions by sending Update messages with topic `agent.suggestion`

## Suggestion Payload Schema

```json
{
  "type": "news_highlight | anomaly | summary | correlation_break",
  "timestamp": "2024-01-15T10:30:00Z",
  "instrument": "BTCUSD",
  "title": "Short human-readable title",
  "body": "Detailed explanation",
  "urgency": "high | medium | low",
  "metadata": {}
}
```
