// Centralized data store — mirrors TUI Model exactly.
// Connects to server via SSE (EventSource).

import { useEffect, useState, useCallback } from "react";

// === Data types (same as TUI) ===

export interface Candle {
  time: number; open: number; high: number; low: number; close: number; volume: number;
}

export interface InstrumentData {
  instrument: string; exchange: string;
  timeframes: Map<string, Candle[]>;
  activeTF: string; lastUpdate: number;
}

export interface LOBLevel { price: number; qty: number; }

export interface LOBData {
  instrument: string; exchange: string;
  bids: LOBLevel[]; asks: LOBLevel[];
  lastUpdate: number;
}

export interface NewsItem {
  title: string; source: string; body: string; url: string;
  tickers: string[]; timestamp: number;
}

export interface FeedStatus {
  name: string; state: string; latencyMs: number; errorCount: number;
}

export interface AlertEntry {
  id: string; type: string; instrument: string; status: string; timestamp: number;
}

// === Store interface ===

export interface Store {
  connected: boolean;
  ohlcData: Map<string, InstrumentData>;
  ohlcKeys: string[];
  ohlcActiveIdx: number;
  setOhlcActiveIdx: (idx: number) => void;
  cycleTF: (dir: number) => void;
  lobData: Map<string, LOBData>;
  lobKeys: string[];
  lobActiveIdx: number;
  setLobActiveIdx: (idx: number) => void;
  newsItems: NewsItem[];
  alertItems: AlertEntry[];
  feedStatuses: FeedStatus[];
  logLines: string[];
  fetchOHLCHistory: (instrument: string, exchange: string, tf: string) => Promise<void>;
}

// === Helpers ===

function instrumentKey(inst: string, ex: string) { return `${inst}/${ex}`; }

const TF_ORDER = ["1m", "5m", "15m", "1h", "4h", "1d", "spot"];

function getToken(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get("token") || "";
}

// === Hook ===

export function useStore(): Store {
  const [connected, setConnected] = useState(false);
  const [ohlcData, setOhlcData] = useState<Map<string, InstrumentData>>(new Map());
  const [ohlcKeys, setOhlcKeys] = useState<string[]>([]);
  const [ohlcActiveIdx, setOhlcActiveIdx] = useState(0);
  const [lobData, setLobData] = useState<Map<string, LOBData>>(new Map());
  const [lobKeys, setLobKeys] = useState<string[]>([]);
  const [lobActiveIdx, setLobActiveIdx] = useState(0);
  const [newsItems, setNewsItems] = useState<NewsItem[]>([]);
  const [alertItems, setAlertItems] = useState<AlertEntry[]>([]);
  const [feedStatuses, setFeedStatuses] = useState<FeedStatus[]>([]);
  const [logLines, setLogLines] = useState<string[]>([]);

  const cycleTF = useCallback((dir: number) => {
    setOhlcData((prev) => {
      const keys = Array.from(prev.keys());
      if (!keys.length) return prev;
      const key = keys[ohlcActiveIdx] || keys[0];
      const inst = prev.get(key);
      if (!inst) return prev;
      const avail = TF_ORDER.filter((tf) => inst.timeframes.has(tf));
      if (!avail.length) return prev;
      const cur = avail.indexOf(inst.activeTF);
      const next = (cur + dir + avail.length) % avail.length;
      const m = new Map(prev);
      m.set(key, { ...inst, activeTF: avail[next] });
      return m;
    });
  }, [ohlcActiveIdx]);

  useEffect(() => {
    const token = getToken();
    const url = `http://localhost:9474/api/v1/subscribe?patterns=ohlc.*.*,lob.*.*,news,alert,feed.status,server.log&token=${token}`;

    console.log("[store] connecting SSE", url.substring(0, 80));
    const es = new EventSource(url);

    es.onopen = () => {
      console.log("[store] SSE open");
      setConnected(true);
      // Fetch existing news from server on connect.
      fetch(`http://localhost:9474/api/v1/snapshot?topic=news&limit=200&token=${token}`)
        .then((r) => r.json())
        .then((data) => {
          if (Array.isArray(data)) {
            const items = data.filter((p: any) => p.Title).map((p: any) => ({
              title: p.Title, source: p.Source || "", body: p.Body || "",
              url: p.URL || "", tickers: p.Tickers || [], timestamp: Date.now() / 1000,
            }));
            setNewsItems((prev) => {
              const seen = new Set(prev.map((n) => n.title));
              const newItems = items.filter((n: any) => !seen.has(n.title));
              return [...newItems, ...prev].slice(0, 1000);
            });
          }
        })
        .catch(() => {});
    };
    es.onerror = () => { console.log("[store] SSE error"); setConnected(false); };

    es.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        const topic: string = msg._topic || "";
        const p = msg._payload || {};

        // === handleServerData — same logic as TUI app.go ===

        if (topic.startsWith("ohlc.")) {
          const key = instrumentKey(p.Instrument, p.Exchange);
          const tf = p.Timeframe || "1m";
          let ts: number;
          if (typeof p.Timestamp === "string") ts = Math.floor(new Date(p.Timestamp).getTime() / 1000);
          else ts = p.Timestamp > 1e12 ? Math.floor(p.Timestamp / 1000) : (p.Timestamp || Math.floor(Date.now() / 1000));

          const candle: Candle = { time: ts, open: p.Open, high: p.High, low: p.Low, close: p.Close, volume: p.Volume || 0 };

          setOhlcData((prev) => {
            const m = new Map(prev);
            let inst = m.get(key);
            if (!inst) {
              inst = { instrument: p.Instrument, exchange: p.Exchange, timeframes: new Map(), activeTF: tf, lastUpdate: Date.now() };
              m.set(key, inst);
              setOhlcKeys((k) => k.includes(key) ? k : [...k, key]);
            }
            const candles = inst.timeframes.get(tf) || [];
            const idx = candles.findIndex((c) => c.time === ts);
            if (idx >= 0) candles[idx] = candle; else candles.push(candle);
            const tfs = new Map(inst.timeframes);
            tfs.set(tf, candles.slice(-200));
            m.set(key, { ...inst, timeframes: tfs, lastUpdate: Date.now() });
            return m;
          });

        } else if (topic.startsWith("lob.")) {
          const key = instrumentKey(p.Instrument, p.Exchange);
          const bids = (p.Bids || []).map((b: any) => ({ price: b.Price, qty: b.Quantity }));
          const asks = (p.Asks || []).map((a: any) => ({ price: a.Price, qty: a.Quantity }));
          setLobData((prev) => {
            const m = new Map(prev);
            m.set(key, { instrument: p.Instrument, exchange: p.Exchange, bids, asks, lastUpdate: Date.now() });
            setLobKeys((k) => k.includes(key) ? k : [...k, key]);
            return m;
          });

        } else if (topic === "news" && p.Title) {
          setNewsItems((prev) => {
            if (prev.some((n) => n.title === p.Title)) return prev;
            return [{ title: p.Title, source: p.Source || "", body: p.Body || "", url: p.URL || "", tickers: p.Tickers || [], timestamp: Date.now() / 1000 }, ...prev].slice(0, 1000);
          });

        } else if (topic === "alert") {
          setAlertItems((prev) => [{ id: p.ID || String(Date.now()), type: p.Type || "", instrument: p.Instrument || "", status: p.Status || "", timestamp: Date.now() / 1000 }, ...prev].slice(0, 50));

        } else if (topic === "server.log") {
          const line = `${p.Time || ""} [${p.Level || ""}] ${p.Message || ""}`;
          setLogLines((prev) => [...prev, line].slice(-500));

        } else if (topic === "feed.status") {
          setFeedStatuses((prev) => {
            const idx = prev.findIndex((f) => f.name === p.Name);
            const entry: FeedStatus = { name: p.Name, state: p.State || "unknown", latencyMs: p.LatencyMs || 0, errorCount: p.ErrorCount || 0 };
            if (idx >= 0) { const next = [...prev]; next[idx] = entry; return next; }
            return [...prev, entry].sort((a, b) => a.name.localeCompare(b.name));
          });
        }
      } catch { /* skip bad JSON */ }
    };

    return () => es.close();
  }, []);

  // Fetch OHLC history from server cache for a specific instrument/tf.
  const fetchOHLCHistory = useCallback(async (instrument: string, exchange: string, tf: string) => {
    try {
      const token = getToken();
      const query = `ohlc/${exchange}/${instrument}`;
      const resp = await fetch(`http://localhost:9474/api/v1/snapshot?topic=ohlc.${exchange}.${instrument}&limit=200&token=${token}`);
      const data = await resp.json();
      if (!Array.isArray(data) || data.length === 0) return;

      const key = instrumentKey(instrument, exchange);
      setOhlcData((prev) => {
        const m = new Map(prev);
        let inst = m.get(key);
        if (!inst) return prev;

        const candles: Candle[] = data
          .filter((p: any) => (p.Timeframe || "1m") === tf)
          .map((p: any) => {
            let ts: number;
            if (typeof p.Timestamp === "string") ts = Math.floor(new Date(p.Timestamp).getTime() / 1000);
            else ts = p.Timestamp > 1e12 ? Math.floor(p.Timestamp / 1000) : p.Timestamp;
            return { time: ts, open: p.Open, high: p.High, low: p.Low, close: p.Close, volume: p.Volume || 0 };
          })
          .sort((a: Candle, b: Candle) => a.time - b.time);

        if (candles.length === 0) return prev;

        const tfs = new Map(inst.timeframes);
        const existing = tfs.get(tf) || [];
        // Merge: keep existing, add history before.
        const merged = [...candles, ...existing];
        const deduped = Array.from(new Map(merged.map((c) => [c.time, c])).values()).sort((a, b) => a.time - b.time).slice(-200);
        tfs.set(tf, deduped);
        m.set(key, { ...inst, timeframes: tfs, activeTF: tf });
        return m;
      });
    } catch {}
  }, []);

  return {
    connected, ohlcData, ohlcKeys, ohlcActiveIdx, setOhlcActiveIdx, cycleTF,
    lobData, lobKeys, lobActiveIdx, setLobActiveIdx,
    newsItems, alertItems, feedStatuses, logLines,
    fetchOHLCHistory,
  };
}
