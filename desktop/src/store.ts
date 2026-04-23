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

export interface PluginScreen {
  id: string; plugin: string; label: string; icon: string; topic: string;
}

export interface PluginStyledLine {
  text: string; style: string;
}

export interface CellAddress { row: number; col: number; }
export interface CellStyle { fg?: string; bg?: string; bold?: boolean; italic?: boolean; underline?: boolean; }
export interface EnumOption { value: string; label: string; }
export interface PluginCell {
  address: CellAddress;
  style?: CellStyle;
  type: string; // "text", "input_decimal", "input_enum", "number", etc.
  text?: string;
  label?: string;
  value?: any;
  precision?: number;
  unit?: string;
  delta?: string;
  options?: EnumOption[];
  expression?: string;
  component_id?: string;
  visible_when?: string;
  col_span?: number;
  row_span?: number;
}

// Trade aggregates (server-computed, not raw trades).
export interface TradeAgg {
  Instrument: string; Exchange: string;
  Count: number; Volume: number; BuyVolume: number; SellVolume: number;
  VWAP: number; Open: number; High: number; Low: number; Close: number;
  Turnover: number; P25: number; P50: number; P75: number;
}

export interface TradeSnapEntry {
  Price: number; Quantity: number; Side: string; Timestamp: string;
}

export interface TradeSnapData {
  Instrument: string; Exchange: string; Trades: TradeSnapEntry[];
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
  tradeAggs: Record<string, TradeAgg>;        // key: exchange/instrument
  tradeSnaps: Record<string, TradeSnapData>;  // key: exchange/instrument
  tradeKeys: string[];
  pluginScreens: PluginScreen[];
  pluginLines: Record<string, PluginStyledLine[]>;
  pluginCells: Record<string, PluginCell[]>;
  msgCount: number; // total SSE messages received this session, capped at 50000 (TUI parity)
  fetchOHLCHistory: (instrument: string, exchange: string, tf: string) => Promise<void>;

  // Progressive history over /api/v1/datarange — streams NDJSON
  // chunks into the candles buffer so the chart renders what we
  // have as we have it, instead of blocking on a single snapshot.
  fetchOHLCHistoryStreaming: (instrument: string, exchange: string, tf: string, hours?: number) => Promise<void>;

  // key format: "INSTRUMENT|EXCHANGE|TF". Set to chunks-received
  // count while streaming, absent when idle. Component renders a
  // spinner from this map.
  ohlcLoading: Record<string, number>;
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
  const [tradeAggs, setTradeAggs] = useState<Record<string, TradeAgg>>({});
  const [tradeSnaps, setTradeSnaps] = useState<Record<string, TradeSnapData>>({});
  const [tradeKeys, setTradeKeys] = useState<string[]>([]);
  const [pluginScreens, setPluginScreens] = useState<PluginScreen[]>([]);
  const [pluginLines, setPluginLines] = useState<Record<string, PluginStyledLine[]>>({});
  const [pluginCells, setPluginCells] = useState<Record<string, PluginCell[]>>({});
  const [ohlcLoading, setOhlcLoading] = useState<Record<string, number>>({});
  const [msgCount, setMsgCount] = useState(0);

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
    const url = `http://localhost:9474/api/v1/subscribe?patterns=ohlc.*.*,lob.*.*,trade.agg.*.*,trade.snap.*.*,news,alert,feed.status,server.log,plugin.*,plugin.*.*&token=${token}`;

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
        // Mirror TUI: counter tops out at 50K so long sessions don't
        // bloat state updates. TopBar shows "10234 msgs" / "50000+".
        setMsgCount((c) => (c >= 50000 ? 50000 : c + 1));
        if (topic.startsWith("plugin")) {
          console.log("[store] plugin msg:", topic, JSON.stringify(p).substring(0, 100));
        }

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

        } else if (topic.startsWith("trade.agg.")) {
          const agg = p as TradeAgg;
          const key = `${agg.Exchange}/${agg.Instrument}`;
          setTradeAggs((prev) => ({ ...prev, [key]: agg }));
          setTradeKeys((prev) => prev.includes(key) ? prev : [...prev, key].sort());

        } else if (topic.startsWith("trade.snap.")) {
          const snap = p as TradeSnapData;
          const key = `${snap.Exchange}/${snap.Instrument}`;
          setTradeSnaps((prev) => ({ ...prev, [key]: snap }));

        } else if (topic === "plugin.registry") {
          console.log("[store] plugin.registry received", p.screens?.length, "screens");
          setPluginScreens(p.screens || []);

        } else if (topic.startsWith("plugin.") && topic.endsWith(".screen")) {
          // Cell grid mode (cellgrid/v1).
          if (p.version === "cellgrid/v1" && Array.isArray(p.cells)) {
            setPluginCells((prev) => {
              if (p.full_replace) {
                return { ...prev, [topic]: p.cells };
              }
              // Incremental merge.
              const existing = prev[topic] || [];
              const idx = new Map(existing.map((c: PluginCell, i: number) => [`${c.address.row},${c.address.col}`, i]));
              const merged = [...existing];
              for (const c of p.cells as PluginCell[]) {
                const key = `${c.address.row},${c.address.col}`;
                const i = idx.get(key);
                if (i !== undefined) { merged[i] = c; } else { merged.push(c); }
              }
              return { ...prev, [topic]: merged };
            });
          } else {
            // Legacy styled-line mode.
            const lines: PluginStyledLine[] = (p.lines || []).map((l: any) => ({ text: l.text || "", style: l.style || "normal" }));
            setPluginLines((prev) => ({ ...prev, [topic]: lines }));
          }
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

  // Progressive streaming fetch — reads /api/v1/datarange as NDJSON
  // and appends chunks to the candles buffer as they arrive. Sets
  // ohlcLoading[key] to the live chunk count while in flight. Each
  // chunk triggers one React update, so the chart re-renders
  // incrementally.
  const fetchOHLCHistoryStreaming = useCallback(async (instrument: string, exchange: string, tf: string, hours = 24) => {
    const token = getToken();
    const loadKey = `${instrument}|${exchange}|${tf}`;
    if (ohlcLoading[loadKey] !== undefined) return; // already running
    setOhlcLoading((prev) => ({ ...prev, [loadKey]: 0 }));

    try {
      const to = new Date();
      const from = new Date(to.getTime() - hours * 3600 * 1000);
      const q = new URLSearchParams({
        topic: `ohlc.${exchange}.${instrument}`,
        from: from.toISOString().replace(/\.\d+Z$/, "Z"),
        to: to.toISOString().replace(/\.\d+Z$/, "Z"),
        correlation_id: loadKey,
        max_records: "5000",
        token,
      });
      const resp = await fetch(`http://localhost:9474/api/v1/datarange?${q.toString()}`);
      if (!resp.ok || !resp.body) {
        console.warn("[store] datarange bad response", resp.status);
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let chunks = 0;
      const key = instrumentKey(instrument, exchange);

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        // NDJSON: split on newlines, keep the trailing partial.
        let idx: number;
        while ((idx = buffer.indexOf("\n")) >= 0) {
          const line = buffer.slice(0, idx);
          buffer = buffer.slice(idx + 1);
          if (!line.trim()) continue;

          let chunk: any;
          try { chunk = JSON.parse(line); } catch { continue; }

          if (chunk.EOF) continue;

          const records: any[] = chunk.Records || [];
          if (records.length === 0) continue;

          // Parse the payload for each record; filter by tf.
          const newCandles: Candle[] = [];
          for (const r of records) {
            let p: any;
            try { p = typeof r.payload === "string" ? JSON.parse(r.payload) : r.payload; }
            catch { continue; }
            if (!p) continue;
            if ((p.Timeframe || "1m") !== tf) continue;
            let ts: number;
            if (typeof p.Timestamp === "string") ts = Math.floor(new Date(p.Timestamp).getTime() / 1000);
            else ts = p.Timestamp > 1e12 ? Math.floor(p.Timestamp / 1000) : p.Timestamp;
            newCandles.push({ time: ts, open: p.Open, high: p.High, low: p.Low, close: p.Close, volume: p.Volume || 0 });
          }
          if (newCandles.length === 0) continue;

          chunks++;
          setOhlcLoading((prev) => ({ ...prev, [loadKey]: chunks }));
          setOhlcData((prev) => {
            const m = new Map(prev);
            const inst = m.get(key);
            if (!inst) return prev;
            const tfs = new Map(inst.timeframes);
            const existing = tfs.get(tf) || [];
            const merged = [...newCandles, ...existing];
            const deduped = Array.from(new Map(merged.map((c) => [c.time, c])).values())
              .sort((a, b) => a.time - b.time).slice(-2000);
            tfs.set(tf, deduped);
            m.set(key, { ...inst, timeframes: tfs, activeTF: tf });
            return m;
          });
        }
      }
    } catch (e) {
      console.warn("[store] datarange stream error", e);
    } finally {
      setOhlcLoading((prev) => {
        const next = { ...prev };
        delete next[loadKey];
        return next;
      });
    }
  }, [ohlcLoading]);

  return {
    connected, ohlcData, ohlcKeys, ohlcActiveIdx, setOhlcActiveIdx, cycleTF,
    lobData, lobKeys, lobActiveIdx, setLobActiveIdx,
    newsItems, alertItems, feedStatuses, logLines,
    tradeAggs, tradeSnaps, tradeKeys,
    pluginScreens, pluginLines, pluginCells,
    fetchOHLCHistory, fetchOHLCHistoryStreaming, ohlcLoading, msgCount,
  };
}
