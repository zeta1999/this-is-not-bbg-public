// Pure reducer / parser helpers used by phone/src/store.ts.
//
// Extracted into its own module so the bug-prone bits (news
// dedup, cell-grid dedup, alert merge, ohlc change% calc, trade
// snap trim) can be unit-tested without spinning up Jest's
// react-native-sse + EventSource shim. The store is just
// composition of these reducers + a thin SSE adapter.

import type {
  AlertEntry,
  FeedStatus,
  NewsItem,
  PriceEntry,
  SanitySnapshot,
  TradeEntry,
} from "./store";

export interface PluginCellLike {
  address: { row: number; col: number };
  type?: string;
  [key: string]: any;
}

export const MAX_VISIBLE_TRADES = 30;
// News/alert caps are memory-bound safety nets, not UX limits — the
// user explicitly does not want session history truncated at 100/200.
// These sit well above any plausible long-running session count.
export const MAX_NEWS_ITEMS = 10000;
export const MAX_ALERT_ENTRIES = 10000;

// ---------------------------------------------------------------
// News
// ---------------------------------------------------------------

// parseNewsBackfill takes the raw /api/v1/snapshot?topic=news
// payload (an array of records with mixed casing) and produces a
// dedup-by-title, newest-first NewsItem list capped at the
// in-memory cap. Used at SSE-connect time so the NEWS tab lands
// populated rather than waiting for the next live story.
export function parseNewsBackfill(raw: unknown): NewsItem[] {
  if (!Array.isArray(raw)) return [];
  const items: NewsItem[] = [];
  for (const p of raw) {
    if (!p || typeof p !== "object") continue;
    const title = (p as any).Title || (p as any).title || "";
    if (!title) continue;
    const ts: number =
      typeof (p as any).Published === "string"
        ? new Date((p as any).Published).getTime() / 1000
        : Date.now() / 1000;
    items.push({
      title,
      source: (p as any).Source || (p as any).source || "",
      body: (p as any).Body || (p as any).body || "",
      url: (p as any).URL || (p as any).url || "",
      tickers: (p as any).Tickers || (p as any).tickers || [],
      timestamp: isFinite(ts) ? ts : Date.now() / 1000,
    });
  }
  const seen = new Set<string>();
  const deduped: NewsItem[] = [];
  for (const n of items) {
    if (seen.has(n.title)) continue;
    seen.add(n.title);
    deduped.push(n);
  }
  deduped.sort((a, b) => b.timestamp - a.timestamp);
  return deduped.slice(0, MAX_NEWS_ITEMS);
}

// upsertLiveNews handles a single SSE "news" message. Title-based
// dedup against the existing list; sort DESC by timestamp so the
// list stays ordered when a backfill replay races a live arrival;
// cap at MAX_NEWS_ITEMS so a long-running session doesn't blow up
// the state object.
export function upsertLiveNews(prev: NewsItem[], payload: any, nowSec: number): NewsItem[] {
  const title = payload?.Title || payload?.title || "";
  if (!title) return prev;
  if (prev.some((n) => n.title === title)) return prev;
  const pub = payload?.Published || payload?.published;
  const parsed = typeof pub === "string" ? Date.parse(pub) / 1000 : NaN;
  const ts = isFinite(parsed) ? parsed : nowSec;
  const next: NewsItem = {
    title,
    source: payload.Source || payload.source || "",
    body: payload.Body || payload.body || "",
    url: payload.URL || payload.url || "",
    tickers: payload.Tickers || payload.tickers || [],
    timestamp: ts,
  };
  const merged = [next, ...prev];
  merged.sort((a, b) => b.timestamp - a.timestamp);
  return merged.slice(0, MAX_NEWS_ITEMS);
}

// ---------------------------------------------------------------
// Plugin cell grid
// ---------------------------------------------------------------

// dedupCellsByAddr collapses a cell array down to one cell per
// (row, col) — last write wins. Some plugins emit transiently
// overlapping addresses during partial-update batches; without
// this dedup React throws "Encountered two children with the
// same key" and the older payload flickers under the newer one.
export function dedupCellsByAddr<T extends PluginCellLike>(cells: T[]): T[] {
  const map = new Map<string, T>();
  for (const c of cells) {
    map.set(`${c.address.row}-${c.address.col}`, c);
  }
  return Array.from(map.values());
}

// ---------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------

export function upsertAlert(
  prev: AlertEntry[],
  payload: any,
  nowMs: number,
): AlertEntry[] {
  const entry: AlertEntry = {
    id: payload?.ID || payload?.id || String(nowMs),
    message: payload?.Message || payload?.message || "",
    severity: payload?.Severity || payload?.severity || "info",
    timestamp: nowMs,
  };
  return [entry, ...prev].slice(0, MAX_ALERT_ENTRIES);
}

// ---------------------------------------------------------------
// Feed status
// ---------------------------------------------------------------

// upsertFeedStatus replaces an existing record by name, otherwise
// inserts and re-sorts alphabetically. Sort guarantees a stable
// MON-tab ordering across reconnects.
export function upsertFeedStatus(prev: FeedStatus[], payload: any): FeedStatus[] {
  const entry: FeedStatus = {
    name: payload?.Name || payload?.name || "",
    state: payload?.State || payload?.state || "unknown",
    latencyMs: payload?.LatencyMs ?? payload?.latencyMs ?? 0,
    errorCount: payload?.ErrorCount ?? payload?.errorCount ?? 0,
  };
  const idx = prev.findIndex((f) => f.name === entry.name);
  if (idx >= 0) {
    const next = prev.slice();
    next[idx] = entry;
    return next;
  }
  return [...prev, entry].sort((a, b) => a.name.localeCompare(b.name));
}

// ---------------------------------------------------------------
// OHLC tick
// ---------------------------------------------------------------

// applyPriceTick computes change% from the prior price for the
// same key. First tick has no baseline so change=0; thereafter
// uses the stored .price as the baseline. Returns null when the
// payload is missing required fields, so the caller can skip
// state-update churn.
export function applyPriceTick(
  prev: PriceEntry | undefined,
  payload: any,
  nowMs: number,
): PriceEntry | null {
  const close = payload?.Close;
  if (typeof close !== "number" || !isFinite(close)) return null;
  if (!payload?.Instrument || !payload?.Exchange) return null;
  const baseline = prev?.price ?? close;
  const change = baseline > 0 ? ((close - baseline) / baseline) * 100 : 0;
  return {
    instrument: payload.Instrument,
    exchange: payload.Exchange,
    price: close,
    change,
    timeframe: payload.Timeframe || "spot",
    lastUpdate: nowMs,
  };
}

// ---------------------------------------------------------------
// Trade snap
// ---------------------------------------------------------------

// trimTradeSnap caps the rendered tail at MAX_VISIBLE_TRADES so a
// hot symbol (~100 trades/s) doesn't stall the JS thread on
// Android (the original ANR repro from 2026-04-22).
export function trimTradeSnap(trades: TradeEntry[] | undefined): TradeEntry[] {
  if (!Array.isArray(trades)) return [];
  return trades.length > MAX_VISIBLE_TRADES
    ? trades.slice(-MAX_VISIBLE_TRADES)
    : trades;
}

// ---------------------------------------------------------------
// Sanity
// ---------------------------------------------------------------

// parseSanitySnapshot copies the SSE payload into the strongly-
// typed shape the SANITY tab expects. Returns null on a malformed
// payload so the caller can no-op without churning state.
export function parseSanitySnapshot(payload: any, nowMs: number): SanitySnapshot | null {
  if (!payload?.instrument || typeof payload.median !== "number") return null;
  return {
    instrument: payload.instrument,
    median: payload.median,
    threshold_pct: payload.threshold_pct ?? 0,
    venue_count: payload.venue_count ?? 0,
    outlier_count: payload.outlier_count ?? 0,
    venues: Array.isArray(payload.venues) ? payload.venues : [],
    timestamp: payload.timestamp || "",
    lastUpdate: nowMs,
  };
}

// ---------------------------------------------------------------
// Misc
// ---------------------------------------------------------------

// shallowEqualArrays is the cheap-equality used to suppress
// pointless setKeys updates when the underlying id-set hasn't
// changed. Cuts re-renders for high-frequency tickers.
export function shallowEqualArrays(a: string[], b: string[]): boolean {
  if (a === b) return true;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
