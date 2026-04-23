// Phone data store — same SSE EventSource as desktop, read-only.
// Connects to server HTTP gateway for live market data.

import { useEffect, useState, useCallback } from "react";

export interface PriceEntry {
  instrument: string;
  exchange: string;
  price: number;
  change: number;
  timeframe: string;
  lastUpdate: number;
}

export interface LOBLevel { price: number; qty: number; }

export interface LOBData {
  instrument: string;
  exchange: string;
  bids: LOBLevel[];
  asks: LOBLevel[];
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

export interface PluginScreen {
  id: string; plugin: string; label: string; icon: string; topic: string;
}

export interface PluginStyledLine {
  text: string; style: string;
}

export interface PhoneStore {
  connected: boolean;
  prices: Map<string, PriceEntry>;
  priceKeys: string[];
  lobData: Map<string, LOBData>;
  lobKeys: string[];
  newsItems: NewsItem[];
  feedStatuses: FeedStatus[];
  pluginScreens: PluginScreen[];
  pluginLines: Record<string, PluginStyledLine[]>;
}

// Server address — set after QR pairing.
let serverUrl = "";

export function setServerUrl(url: string) { serverUrl = url; }
export function getServerUrl() { return serverUrl; }

export function usePhoneStore(token: string): PhoneStore {
  const [connected, setConnected] = useState(false);
  const [prices, setPrices] = useState<Map<string, PriceEntry>>(new Map());
  const [priceKeys, setPriceKeys] = useState<string[]>([]);
  const [lobData, setLobData] = useState<Map<string, LOBData>>(new Map());
  const [lobKeys, setLobKeys] = useState<string[]>([]);
  const [newsItems, setNewsItems] = useState<NewsItem[]>([]);
  const [feedStatuses, setFeedStatuses] = useState<FeedStatus[]>([]);
  const [pluginScreens, setPluginScreens] = useState<PluginScreen[]>([]);
  const [pluginLines, setPluginLines] = useState<Record<string, PluginStyledLine[]>>({});

  useEffect(() => {
    if (!serverUrl || !token) return;

    const url = `${serverUrl}/api/v1/subscribe?patterns=ohlc.*.*,lob.*.*,news,feed.status,plugin.*,plugin.*.*&token=${token}`;
    const es = new EventSource(url);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    es.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        const topic: string = msg._topic || "";
        const p = msg._payload || {};

        if (topic.startsWith("ohlc.")) {
          const key = `${p.Instrument}/${p.Exchange}`;
          setPrices((prev) => {
            const m = new Map(prev);
            const existing = m.get(key);
            const prevPrice = existing?.price || p.Close;
            const change = prevPrice > 0 ? ((p.Close - prevPrice) / prevPrice * 100) : 0;
            m.set(key, {
              instrument: p.Instrument, exchange: p.Exchange,
              price: p.Close, change,
              timeframe: p.Timeframe || "spot",
              lastUpdate: Date.now(),
            });
            setPriceKeys((k) => k.includes(key) ? k : [...k, key].sort());
            return m;
          });

        } else if (topic.startsWith("lob.")) {
          const key = `${p.Instrument}/${p.Exchange}`;
          const bids = (p.Bids || []).map((b: any) => ({ price: b.Price, qty: b.Quantity }));
          const asks = (p.Asks || []).map((a: any) => ({ price: a.Price, qty: a.Quantity }));
          setLobData((prev) => {
            const m = new Map(prev);
            m.set(key, { instrument: p.Instrument, exchange: p.Exchange, bids, asks });
            setLobKeys((k) => k.includes(key) ? k : [...k, key].sort());
            return m;
          });

        } else if (topic === "news" && p.Title) {
          setNewsItems((prev) => {
            if (prev.some((n) => n.title === p.Title)) return prev;
            return [{
              title: p.Title, source: p.Source || "", body: p.Body || "",
              url: p.URL || "", tickers: p.Tickers || [], timestamp: Date.now() / 1000,
            }, ...prev].slice(0, 500);
          });

        } else if (topic === "feed.status") {
          setFeedStatuses((prev) => {
            const idx = prev.findIndex((f) => f.name === p.Name);
            const entry: FeedStatus = {
              name: p.Name, state: p.State || "unknown",
              latencyMs: p.LatencyMs || 0, errorCount: p.ErrorCount || 0,
            };
            if (idx >= 0) { const next = [...prev]; next[idx] = entry; return next; }
            return [...prev, entry].sort((a, b) => a.name.localeCompare(b.name));
          });

        } else if (topic === "plugin.registry") {
          setPluginScreens(p.screens || []);

        } else if (topic.startsWith("plugin.") && topic.endsWith(".screen")) {
          const lines: PluginStyledLine[] = (p.lines || []).map((l: any) => ({ text: l.text || "", style: l.style || "normal" }));
          setPluginLines((prev) => ({ ...prev, [topic]: lines }));
        }
      } catch {}
    };

    return () => es.close();
  }, [token, serverUrl]);

  return { connected, prices, priceKeys, lobData, lobKeys, newsItems, feedStatuses, pluginScreens, pluginLines };
}
