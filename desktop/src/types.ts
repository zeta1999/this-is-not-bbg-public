// ---------------------------------------------------------------------------
// Data types matching the protobuf schema / Rust backend
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
  status: "ACTIVE" | "TRIGGERED" | "DISABLED";
  alert_type: string;
  condition: string;
  instrument: string;
  triggered_at: number | null;
}

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
  query_type: string;
  instrument?: string;
  timeframe?: string;
  limit?: number;
}

export type TabId = "OHLC" | "LOB" | "NEWS" | "ALERTS" | "MON" | "LOG" | "AGENT";
