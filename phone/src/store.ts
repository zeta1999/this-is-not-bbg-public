// Phone data store — shared across tabs via React Context so the
// SSE EventSource opens exactly once for the whole app.
//
// Migrated from per-tab polling 2026-04-27. The poll-every-3s
// pattern in each tab burned bandwidth and made the latency floor
// 3 seconds; SSE delivers updates inside the human-eye window.

import React, { createContext, useContext, useEffect, useReducer, useRef, useState } from "react";
import EventSource from "react-native-sse";
import { getServerUrl, getToken, isLoaded, onConnectionChange } from "./connection";
import {
  applyPriceTick,
  parseNewsBackfill,
  parseSanitySnapshot,
  shallowEqualArrays as shallowEqualArraysR,
  trimTradeSnap,
  upsertAlert as upsertAlertR,
  upsertFeedStatus as upsertFeedStatusR_,
  upsertLiveNews,
} from "./reducers";

export interface PriceEntry {
  instrument: string;
  exchange: string;
  price: number;
  change: number;
  timeframe: string;
  lastUpdate: number;
}

export interface LOBLevel { Price: number; Quantity: number; }
export interface LOBSnapshot {
  Instrument: string;
  Exchange: string;
  Bids: LOBLevel[];
  Asks: LOBLevel[];
  LastUpdate?: number;
}

export interface NewsItem {
  title: string;
  source: string;
  body: string;
  url: string;
  tickers: string[];
  timestamp: number;
}

export interface FeedStatus {
  name: string;
  state: string;
  latencyMs: number;
  errorCount: number;
}

export interface AlertEntry {
  id: string;
  message: string;
  severity: string; // "info" | "warning" | "critical"
  timestamp: number;
}

export interface SanityVenue {
  exchange: string;
  mid: number;
  delta_pct: number;
  age_seconds: number;
  outlier: boolean;
}

export interface SanitySnapshot {
  instrument: string;
  median: number;
  threshold_pct: number;
  venue_count: number;
  outlier_count: number;
  venues: SanityVenue[];
  timestamp: string;
  lastUpdate: number;
}

export interface TradeAgg {
  Instrument: string; Exchange: string;
  Count: number; Volume: number; BuyVolume: number; SellVolume: number;
  VWAP: number; Open: number; High: number; Low: number; Close: number;
  Turnover: number; P25: number; P50: number; P75: number;
}

export interface TradeEntry {
  Price: number; Quantity: number; Side: string; Timestamp: string;
}

export interface TradeSnap {
  Instrument: string; Exchange: string; Trades: TradeEntry[];
}

export interface PluginScreen {
  id: string; plugin: string; label: string; icon: string; topic: string;
}

export interface PluginStyledLine { text: string; style: string; }

export interface PluginCell {
  address: { row: number; col: number };
  type: string;
  text?: string;
  label?: string;
  value?: any;
  precision?: number;
  unit?: string;
  delta?: string;
  col_span?: number;
  component_id?: string;
  style?: { fg?: string; bg?: string; bold?: boolean };
  src?: string;
  alt?: string;
  width?: number;
  height?: number;
}

export interface PhoneStore {
  connected: boolean;
  lastEventAt: number;
  prices: Map<string, PriceEntry>;
  priceKeys: string[];
  lobData: Map<string, LOBSnapshot>;
  lobKeys: string[];
  tradeAggs: Record<string, TradeAgg>;
  tradeSnaps: Record<string, TradeSnap>;
  tradeKeys: string[];
  newsItems: NewsItem[];
  feedStatuses: FeedStatus[];
  pluginScreens: PluginScreen[];
  pluginLines: Record<string, PluginStyledLine[]>;
  pluginCells: Record<string, PluginCell[]>;
  sanitySnapshots: Record<string, SanitySnapshot>;
  alerts: AlertEntry[];
}

const MAX_VISIBLE_TRADES = 30;
const SUBSCRIBE_PATTERNS = [
  "ohlc.*.*",
  "lob.*.*",
  "trade.agg.*.*",
  "trade.snap.*.*",
  "news",
  "feed.status",
  "plugin.*",
  "plugin.*.*",
  "sanity.prices",
  "alert",
].join(",");

// -------- internal mutable state, exposed via the hook -----------
const empty: PhoneStore = {
  connected: false,
  lastEventAt: 0,
  prices: new Map(),
  priceKeys: [],
  lobData: new Map(),
  lobKeys: [],
  tradeAggs: {},
  tradeSnaps: {},
  tradeKeys: [],
  newsItems: [],
  feedStatuses: [],
  pluginScreens: [],
  pluginLines: {},
  pluginCells: {},
  sanitySnapshots: {},
  alerts: [],
};

const StoreContext = createContext<PhoneStore>(empty);

export const StoreProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [, forceRaw] = useReducer((n: number) => n + 1, 0);
  const stateRef = useRef<PhoneStore>(empty);
  const [token, setToken] = useState(getToken());
  const [serverUrl, setServerUrl] = useState(getServerUrl());

  // Coalesce SSE-driven re-renders. Every onMessage handler used to
  // trigger force() directly — each EventSource callback runs in
  // its own task so React 18 auto-batching doesn't help, and on
  // Android the JS thread was spending all its time reconciling
  // re-renders instead of draining the message queue (the freeze
  // the user kept hitting). With binance now multiplying the live
  // OHLC topic-rate by 8 (one stream per TF) the original
  // 1-render-per-event design is no longer survivable. Cap renders
  // to ~60 fps via rAF — state mutations still apply synchronously
  // to stateRef so consumers see the latest snapshot on the next
  // frame; we just don't ask React to reconcile faster than the
  // device can paint.
  const forceScheduledRef = useRef(false);
  const force = () => {
    if (forceScheduledRef.current) return;
    forceScheduledRef.current = true;
    const cb = () => {
      forceScheduledRef.current = false;
      forceRaw();
    };
    if (typeof requestAnimationFrame === "function") {
      requestAnimationFrame(cb);
    } else {
      setTimeout(cb, 16);
    }
  };

  // Subscribe to connection.ts changes (token/url).
  useEffect(() => {
    const unsub = onConnectionChange(() => {
      setToken(getToken());
      setServerUrl(getServerUrl());
    });
    return unsub;
  }, []);

  useEffect(() => {
    if (!isLoaded() || !token || !serverUrl) return;

    const url = `${serverUrl}/api/v1/subscribe?patterns=${encodeURIComponent(SUBSCRIBE_PATTERNS)}&token=${encodeURIComponent(token)}`;
    // pollingInterval keeps the lib's auto-retry alive — it's a
    // *fallback* polling when the WS-style stream drops, not the
    // primary delivery mechanism. Default (500ms) means the
    // connection re-establishes within half a second of a drop.
    const es = new EventSource(url);

    const setState = (mut: (s: PhoneStore) => PhoneStore) => {
      stateRef.current = mut(stateRef.current);
      force();
    };

    // SSE only delivers FUTURE messages plus the latest single
    // snapshot per topic via LatestPerTopic — perfect for OHLC /
    // LOB / trade aggs (each instrument is its own topic) but
    // wrong for news, which is one topic with N items in the
    // ring buffer. Pull the news ring buffer once on connect so
    // the user lands on a populated list. Same trick the desktop
    // store has used since day one.
    fetch(`${serverUrl}/api/v1/snapshot?topic=news&limit=500&token=${encodeURIComponent(token)}`)
      .then((r) => r.ok ? r.json() : [])
      .then((data: any) => {
        const deduped = parseNewsBackfill(data);
        if (deduped.length > 0) {
          setState((s) => ({ ...s, newsItems: deduped }));
        }
      })
      .catch(() => {});

    es.addEventListener("open", () => {
      setState((s) => ({ ...s, connected: true }));
    });
    es.addEventListener("error", () => {
      setState((s) => ({ ...s, connected: false }));
    });

    es.addEventListener("message", (ev: any) => {
      const data = ev?.data;
      if (typeof data !== "string") return;
      let msg: any;
      try { msg = JSON.parse(data); } catch { return; }
      const topic: string = msg._topic || "";
      const p = msg._payload || {};
      const now = Date.now();

      if (topic.startsWith("ohlc.")) {
        const key = `${p.Instrument}/${p.Exchange}`;
        setState((s) => {
          const tick = applyPriceTick(s.prices.get(key), p, now);
          if (!tick) return s;
          const m = new Map(s.prices);
          m.set(key, tick);
          const keys = Array.from(m.keys()).sort();
          return {
            ...s,
            prices: m,
            priceKeys: shallowEqualArraysR(s.priceKeys, keys) ? s.priceKeys : keys,
            lastEventAt: now,
          };
        });
      } else if (topic.startsWith("lob.")) {
        const key = `${p.Instrument}/${p.Exchange}`;
        setState((s) => {
          const m = new Map(s.lobData);
          m.set(key, {
            Instrument: p.Instrument, Exchange: p.Exchange,
            Bids: p.Bids || [], Asks: p.Asks || [],
            LastUpdate: now,
          });
          const keys = Array.from(m.keys()).sort();
          return {
            ...s,
            lobData: m,
            lobKeys: shallowEqualArraysR(s.lobKeys, keys) ? s.lobKeys : keys,
            lastEventAt: now,
          };
        });
      } else if (topic.startsWith("trade.agg.")) {
        const key = `${p.Exchange}/${p.Instrument}`;
        setState((s) => {
          const next = { ...s.tradeAggs, [key]: p as TradeAgg };
          const keys = Object.keys(next).sort();
          return {
            ...s,
            tradeAggs: next,
            tradeKeys: shallowEqualArraysR(s.tradeKeys, keys) ? s.tradeKeys : keys,
            lastEventAt: now,
          };
        });
      } else if (topic.startsWith("trade.snap.")) {
        const key = `${p.Exchange}/${p.Instrument}`;
        const trimmed = trimTradeSnap(p.Trades);
        setState((s) => ({
          ...s,
          tradeSnaps: {
            ...s.tradeSnaps,
            [key]: { Instrument: p.Instrument, Exchange: p.Exchange, Trades: trimmed },
          },
          lastEventAt: now,
        }));
      } else if (topic === "news") {
        setState((s) => {
          const next = upsertLiveNews(s.newsItems, p, now / 1000);
          if (next === s.newsItems) return s;
          return { ...s, newsItems: next, lastEventAt: now };
        });
      } else if (topic === "feed.status") {
        setState((s) => ({
          ...s,
          feedStatuses: upsertFeedStatusR_(s.feedStatuses, p),
          lastEventAt: now,
        }));
      } else if (topic === "alert") {
        setState((s) => ({
          ...s,
          alerts: upsertAlertR(s.alerts, p, now),
          lastEventAt: now,
        }));
      } else if (topic === "sanity.prices") {
        const snap = parseSanitySnapshot(p, now);
        if (!snap) return;
        setState((s) => ({
          ...s,
          sanitySnapshots: { ...s.sanitySnapshots, [snap.instrument]: snap },
          lastEventAt: now,
        }));
      } else if (topic === "plugin.registry") {
        setState((s) => ({ ...s, pluginScreens: p.screens || [], lastEventAt: now }));
      } else if (topic.startsWith("plugin.") && topic.endsWith(".screen")) {
        if (Array.isArray(p.cells) && p.cells.length > 0) {
          setState((s) => ({
            ...s,
            pluginCells: { ...s.pluginCells, [topic]: p.cells },
            lastEventAt: now,
          }));
        } else if (Array.isArray(p.lines)) {
          const lines: PluginStyledLine[] = p.lines.map((l: any) => ({
            text: l.text || "", style: l.style || "normal",
          }));
          setState((s) => ({
            ...s,
            pluginLines: { ...s.pluginLines, [topic]: lines },
            lastEventAt: now,
          }));
        }
      }
    });

    return () => {
      try { es.close(); } catch {}
      stateRef.current = { ...stateRef.current, connected: false };
      force();
    };
  }, [token, serverUrl]);

  return React.createElement(StoreContext.Provider, { value: stateRef.current }, children);
};

export function useStore(): PhoneStore {
  return useContext(StoreContext);
}
