/**
 * Phone client for notbbg server.
 * Connects via HTTP/SSE gateway (server port 9474).
 */

import {
  ConnectionStatus,
  PairPayload,
  ServerConfig,
  WatchlistItem,
  NewsItem,
  Alert,
  AlertType,
} from "../types";
import {
  saveServerConfig,
  getServerConfig as getServerConfigInternal,
  clearServerConfig,
  saveSessionToken,
} from "./storage";

// Re-export for consumers.
export { getServerConfig } from "./storage";

// ---------------------------------------------------------------------------
// Connection state
// ---------------------------------------------------------------------------

type InternalState = {
  status: "disconnected" | "connecting" | "connected" | "error";
  baseURL: string | null;
  eventSource: EventSource | null;
  lastError: string | null;
  listeners: Set<() => void>;
};

const state: InternalState = {
  status: "disconnected",
  baseURL: null,
  eventSource: null,
  lastError: null,
  listeners: new Set(),
};

function notify(): void {
  state.listeners.forEach((fn) => fn());
}

export function onConnectionChange(listener: () => void): () => void {
  state.listeners.add(listener);
  return () => {
    state.listeners.delete(listener);
  };
}

export function getConnectionStatus(): ConnectionStatus {
  return {
    connected: state.status === "connected",
    server_addr: state.baseURL ?? "",
    latency_ms: 0,
  };
}

// ---------------------------------------------------------------------------
// Data event listeners
// ---------------------------------------------------------------------------

type DataListener = (topic: string, payload: unknown) => void;
const dataListeners = new Set<DataListener>();

export function onData(listener: DataListener): () => void {
  dataListeners.add(listener);
  return () => dataListeners.delete(listener);
}

// ---------------------------------------------------------------------------
// Connection
// ---------------------------------------------------------------------------

export async function hasPairedServer(): Promise<boolean> {
  const config = await getServerConfigInternal();
  return config !== null;
}

export async function pair(payload: PairPayload): Promise<boolean> {
  // HTTP gateway port = TCP port + 1 by convention
  const baseURL = `http://${payload.host}:${payload.port + 1}`;

  try {
    const resp = await fetch(`${baseURL}/api/v1/health`);
    if (!resp.ok) throw new Error(`Health check failed: ${resp.status}`);

    await saveServerConfig({
      host: payload.host,
      port: payload.port,
      sessionToken: payload.token,
      pairedAt: Date.now(),
    });
    await saveSessionToken(payload.token);

    return true;
  } catch (e: unknown) {
    state.lastError = e instanceof Error ? e.message : String(e);
    return false;
  }
}

export async function connect(): Promise<void> {
  const config = await getServerConfigInternal();
  if (!config) {
    state.status = "error";
    state.lastError = "No server config — pair first";
    notify();
    return;
  }

  const baseURL = `http://${config.host}:${config.port + 1}`;
  state.baseURL = baseURL;
  state.status = "connecting";
  notify();

  try {
    const resp = await fetch(`${baseURL}/api/v1/health`);
    if (!resp.ok) throw new Error(`Server unreachable: ${resp.status}`);

    // Open SSE stream.
    const patterns = "ohlc.*.*,lob.*.*,news,alert,feed.status,indicator.*";
    const es = new EventSource(
      `${baseURL}/api/v1/subscribe?patterns=${encodeURIComponent(patterns)}`
    );

    es.onopen = () => {
      state.status = "connected";
      state.eventSource = es;
      notify();
    };

    es.onerror = () => {
      state.status = "error";
      state.lastError = "SSE connection lost";
      notify();
    };

    es.onmessage = (event: MessageEvent) => {
      try {
        const data = JSON.parse(event.data);
        dataListeners.forEach((fn) => fn("message", data));
      } catch {
        // ignore parse errors
      }
    };

    state.eventSource = es;
  } catch (e: unknown) {
    state.status = "error";
    state.lastError = e instanceof Error ? e.message : String(e);
    notify();
  }
}

export function disconnect(): void {
  if (state.eventSource) {
    state.eventSource.close();
    state.eventSource = null;
  }
  state.status = "disconnected";
  state.baseURL = null;
  notify();
}

export async function unpair(): Promise<void> {
  disconnect();
  await clearServerConfig();
}

// ---------------------------------------------------------------------------
// REST queries
// ---------------------------------------------------------------------------

async function apiGet<T>(path: string): Promise<T> {
  if (!state.baseURL) throw new Error("Not connected");
  const resp = await fetch(`${state.baseURL}${path}`);
  if (!resp.ok) throw new Error(`API error: ${resp.status}`);
  return resp.json();
}

export async function fetchWatchlist(): Promise<WatchlistItem[]> {
  const data = await apiGet<Record<string, unknown>[]>(
    "/api/v1/snapshot?topic=ohlc.coingecko.*&limit=20"
  );
  return data.map((d) => ({
    symbol: String(d.Instrument ?? d.instrument ?? "???"),
    exchange: String(d.Exchange ?? d.exchange ?? "coingecko"),
    lastPrice: Number(d.Close ?? d.close ?? 0),
    change24h: 0,
    changePercent24h: 0,
    volume24h: Number(d.Volume ?? d.volume ?? 0),
    updatedAt: Date.now(),
  }));
}

export async function fetchNews(): Promise<NewsItem[]> {
  const data = await apiGet<Record<string, unknown>[]>(
    "/api/v1/snapshot?topic=news&limit=30"
  );
  return data.map((d, i) => ({
    id: String(d.ID ?? `n${i}`),
    headline: String(d.Title ?? d.title ?? ""),
    source: String(d.Source ?? d.source ?? ""),
    timestamp: Date.now(),
    body: String(d.Body ?? d.body ?? ""),
    tags: (d.Tickers as string[]) ?? [],
  }));
}

export async function fetchAlerts(): Promise<Alert[]> {
  const data = await apiGet<Record<string, unknown>[]>(
    "/api/v1/snapshot?topic=alert&limit=20"
  );
  return data.map((d, i) => ({
    id: String(d.ID ?? `a${i}`),
    alert_type: (String(d.Type ?? "PRICE_ABOVE") as AlertType),
    instrument: String(d.Instrument ?? ""),
    condition: String(d.Condition ?? ""),
    threshold: Number(d.Threshold ?? 0),
    status: (String(d.Status ?? "ACTIVE") as Alert["status"]),
    created_at: Date.now(),
    triggered_at: null,
    payload: "",
  }));
}

export async function createAlert(alert: {
  alert_type: AlertType;
  instrument: string;
  condition: string;
  threshold: number;
}): Promise<void> {
  console.log("Create alert:", alert);
}
