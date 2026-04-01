// ---------------------------------------------------------------------------
// Data types matching the protobuf schema / server backend
// Mirrors desktop/src/types.ts for consistency across clients
// ---------------------------------------------------------------------------

export interface OHLCBar {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface Trade {
  id: string;
  instrument: string;
  price: number;
  size: number;
  side: "BUY" | "SELL";
  timestamp: number;
  exchange: string;
}

export interface LOBLevel {
  price: number;
  size: number;
  count: number;
}

export interface LOBSnapshot {
  instrument: string;
  bids: LOBLevel[];
  asks: LOBLevel[];
  timestamp: number;
}

export interface NewsItem {
  id: string;
  headline: string;
  source: string;
  timestamp: number;
  body: string;
  tags: string[];
}

export interface Alert {
  id: string;
  status: "ACTIVE" | "TRIGGERED" | "DISMISSED";
  alert_type: AlertType;
  condition: string;
  instrument: string;
  threshold: number;
  created_at: number;
  triggered_at: number | null;
  payload: string;
}

export type AlertType =
  | "PRICE_ABOVE"
  | "PRICE_BELOW"
  | "VOLUME_SPIKE"
  | "KEYWORD"
  | "FEED_DOWN";

export interface FeedStatus {
  feed_name: string;
  status: "OK" | "DEGRADED" | "DOWN";
  latency_ms: number;
  messages_per_sec: number;
  errors: number;
  last_update: number;
}

export interface ConnectionStatus {
  connected: boolean;
  server_addr: string;
  latency_ms: number;
}

export interface SubscribeRequest {
  channel: string;
  instrument?: string;
}

export interface QueryRequest {
  query_type: "OHLC" | "TRADES" | "LOB" | "NEWS" | "ALERTS";
  instrument?: string;
  timeframe?: string;
  limit?: number;
  query?: string;
}

export interface QueryResponse {
  ohlc: OHLCBar[];
  trades: Trade[];
  lob: LOBSnapshot[];
  news: NewsItem[];
  alerts: Alert[];
}

/** QR code payload format for server pairing */
export interface PairPayload {
  host: string;
  port: number;
  token: string;
}

/** Stored server configuration */
export interface ServerConfig {
  host: string;
  port: number;
  sessionToken: string;
  pairedAt: number;
}

/** Instrument summary for watchlist display */
export interface WatchlistItem {
  symbol: string;
  exchange: string;
  lastPrice: number;
  change24h: number;
  changePercent24h: number;
  volume24h: number;
  updatedAt: number;
}
