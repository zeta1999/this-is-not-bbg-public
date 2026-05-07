import React, { useRef, useEffect, useState, useMemo } from "react";
import { createChart, ColorType } from "lightweight-charts";
import type { IChartApi, ISeriesApi, CandlestickData, Time } from "lightweight-charts";
import { colors, fonts } from "../styles/theme";
import type { InstrumentData } from "../store";

interface Props {
  ohlcData: Map<string, InstrumentData>;
  ohlcKeys: string[];
  activeIdx: number;
  setActiveIdx: (idx: number) => void;
  cycleTF: (dir: number) => void;
  fetchOHLCHistory?: (instrument: string, exchange: string, tf: string) => Promise<void>;
  fetchOHLCHistoryStreaming?: (instrument: string, exchange: string, tf: string, hours?: number) => Promise<void>;
  ohlcLoading?: Record<string, number>;
  focusSearchSignal?: number;
}

const TF_ORDER = ["1m", "5m", "15m", "30m", "1h", "4h", "6h", "1d", "1w", "spot"];

export const OHLCChart: React.FC<Props> = ({ ohlcData, ohlcKeys, activeIdx, setActiveIdx, cycleTF, fetchOHLCHistory, fetchOHLCHistoryStreaming, ohlcLoading, focusSearchSignal }) => {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState("");
  // Remember which (instrument|exchange|tf) pairs have already
  // auto-backfilled so we don't re-fetch on every re-render. Lives
  // in a ref so updates don't re-trigger effects.
  const autoBackfilledRef = useRef<Set<string>>(new Set());

  const activeKey = ohlcKeys[activeIdx] || "";
  const activeInst = ohlcData.get(activeKey);
  const activeTF = activeInst?.activeTF || "1m";
  const candles = useMemo(() => activeInst?.timeframes.get(activeTF) || [], [activeInst, activeTF]);
  const availTFs = useMemo(() => activeInst ? TF_ORDER.filter((tf) => activeInst.timeframes.has(tf)) : [], [activeInst]);

  const lastPrice = candles.length > 0 ? candles[candles.length - 1].close : 0;
  const change = candles.length > 1
    ? ((candles[candles.length - 1].close - candles[candles.length - 2].close) / candles[candles.length - 2].close * 100) : 0;

  // Sorted + filtered sidebar items. Matches TUI searchInstrument():
  // substring match on instrument, exchange, or combined key.
  const sidebarItems = useMemo(() => {
    const q = search.toLowerCase();
    return ohlcKeys
      .map((key, idx) => ({ key, idx, inst: ohlcData.get(key) }))
      .filter(({ key, inst }) => !q
        || (inst?.instrument || "").toLowerCase().includes(q)
        || (inst?.exchange || "").toLowerCase().includes(q)
        || key.toLowerCase().includes(q))
      .sort((a, b) => (a.inst?.instrument || "").localeCompare(b.inst?.instrument || ""));
  }, [ohlcKeys, ohlcData, search]);

  // `/` keypress from App: focus the search input (TUI parity).
  useEffect(() => {
    if (focusSearchSignal === undefined || focusSearchSignal === 0) return;
    const el = searchInputRef.current;
    if (!el) return;
    el.focus();
    el.select();
  }, [focusSearchSignal]);

  const onSearchKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      setSearch("");
      searchInputRef.current?.blur();
      e.preventDefault();
      return;
    }
    if (e.key === "Enter") {
      // Jump to the first filtered match, then blur so [/] cycling
      // works again without the input eating keys.
      const first = sidebarItems[0];
      if (first) setActiveIdx(first.idx);
      searchInputRef.current?.blur();
      e.preventDefault();
    }
  };

  // Create chart.
  useEffect(() => {
    if (!chartContainerRef.current) return;
    if (chartRef.current) { try { chartRef.current.remove(); } catch {} }

    const chart = createChart(chartContainerRef.current, {
      width: chartContainerRef.current.clientWidth,
      height: chartContainerRef.current.clientHeight,
      watermark: { visible: false },
      layout: { background: { type: ColorType.Solid, color: colors.bg }, textColor: colors.dimText, fontFamily: fonts.mono, fontSize: 11, attributionLogo: false },
      grid: { vertLines: { color: "#1a1a1a" }, horzLines: { color: "#1a1a1a" } },
      crosshair: { vertLine: { color: colors.amber, width: 1, style: 2, labelBackgroundColor: colors.amber }, horzLine: { color: colors.amber, width: 1, style: 2, labelBackgroundColor: colors.amber } },
      rightPriceScale: { borderColor: colors.border },
      timeScale: { borderColor: colors.border, timeVisible: true },
    });
    const series = chart.addCandlestickSeries({
      upColor: colors.green, downColor: colors.red,
      borderUpColor: colors.green, borderDownColor: colors.red,
      wickUpColor: colors.green, wickDownColor: colors.red,
    });
    chartRef.current = chart;
    seriesRef.current = series;

    const ro = new ResizeObserver((entries) => {
      for (const e of entries) chart.applyOptions({ width: e.contentRect.width, height: e.contentRect.height });
    });
    ro.observe(chartContainerRef.current);
    return () => { ro.disconnect(); try { chart.remove(); } catch {} chartRef.current = null; seriesRef.current = null; };
  }, []);

  // Auto-backfill last 24h on first view of an (instrument, tf)
  // pair when the live buffer is sparse. Mirrors the TUI's H key
  // but runs automatically so the chart isn't empty on fresh
  // connect. Non-blocking — the streaming fetch populates the
  // chart as chunks arrive.
  useEffect(() => {
    if (!activeInst || !fetchOHLCHistoryStreaming) return;
    const pairKey = `${activeInst.instrument}|${activeInst.exchange}|${activeTF}`;
    if (autoBackfilledRef.current.has(pairKey)) return;
    if (candles.length >= 50) {
      // Already has a usable live buffer — skip the backfill.
      autoBackfilledRef.current.add(pairKey);
      return;
    }
    autoBackfilledRef.current.add(pairKey);
    fetchOHLCHistoryStreaming(activeInst.instrument, activeInst.exchange, activeTF, 24).catch(() => {});
  }, [activeInst, activeTF, candles.length, fetchOHLCHistoryStreaming]);

  // Track the last (key, tf, lastCandleTime) we rendered. On a
  // hot symbol the candles array gets a new tail entry on every
  // trade tick — calling setData + fitContent for each tick
  // re-pays the full sort + price-axis recompute. lightweight-
  // charts has series.update(point) for incremental in-place
  // updates; only fall back to setData when the active
  // instrument or timeframe changes (or on first render).
  const prevSigRef = useRef<{ key: string; tf: string } | null>(null);
  const lastCandleTimeRef = useRef<number>(0);
  // Tracks the candles-array length we last flushed to lightweight-
  // charts. Used to detect mid-array growth (out-of-order arrivals)
  // so we can fall back to setData instead of dropping the new bars
  // via update(). See the comment in the update effect below.
  const prevLengthRef = useRef<number>(0);

  useEffect(() => {
    if (!seriesRef.current || !chartRef.current) return;
    try {
      if (candles.length === 0) {
        seriesRef.current.setData([]);
        prevSigRef.current = { key: activeKey, tf: activeTF };
        lastCandleTimeRef.current = 0;
        return;
      }
      const sig = { key: activeKey, tf: activeTF };
      const prev = prevSigRef.current;
      const isSameSeries = prev && prev.key === sig.key && prev.tf === sig.tf;

      if (!isSameSeries) {
        // Series changed — full reset.
        const deduped = new Map<number, typeof candles[0]>();
        for (const c of candles) {
          if (c.open > 0 || c.close > 0) deduped.set(c.time, c);
        }
        const sorted = Array.from(deduped.values()).sort((a, b) => a.time - b.time);
        if (sorted.length === 0) { seriesRef.current.setData([]); return; }
        const data: CandlestickData<Time>[] = sorted.map((c) => ({
          time: c.time as Time, open: c.open, high: c.high, low: c.low, close: c.close,
        }));
        seriesRef.current.setData(data);
        chartRef.current.timeScale().fitContent();
        prevSigRef.current = sig;
        lastCandleTimeRef.current = sorted[sorted.length - 1].time;
        prevLengthRef.current = candles.length;
        return;
      }

      // Same series — fast path is `update(last)` for live ticks.
      // But if a snapshot bar arrived out of order (e.g. bitget's WS
      // subscription replays ~100 bars at once that didn't make it
      // into the very first setData) the array can grow on the
      // INSIDE, not just the tail. lightweight-charts' update()
      // refuses any time earlier than the current series' last bar,
      // so a non-tail growth would silently drop those bars. Detect
      // by length-grew-by-more-than-1 and fall back to setData.
      const last = candles[candles.length - 1];
      if (!last || (last.open <= 0 && last.close <= 0)) return;
      const grewBeyondTail = candles.length > prevLengthRef.current + 1;
      if (grewBeyondTail) {
        const deduped = new Map<number, typeof candles[0]>();
        for (const c of candles) {
          if (c.open > 0 || c.close > 0) deduped.set(c.time, c);
        }
        const sorted = Array.from(deduped.values()).sort((a, b) => a.time - b.time);
        const data: CandlestickData<Time>[] = sorted.map((c) => ({
          time: c.time as Time, open: c.open, high: c.high, low: c.low, close: c.close,
        }));
        seriesRef.current.setData(data);
        chartRef.current.timeScale().fitContent();
        if (sorted.length > 0) lastCandleTimeRef.current = sorted[sorted.length - 1].time;
        prevLengthRef.current = candles.length;
        return;
      }
      seriesRef.current.update({
        time: last.time as Time, open: last.open, high: last.high, low: last.low, close: last.close,
      });
      lastCandleTimeRef.current = last.time;
      prevLengthRef.current = candles.length;
    } catch (e) { console.debug("[chart] update error:", e); }
  }, [candles, activeTF, activeKey]);

  // Currency prefix is conditional on knowing the quote currency.
  // Default behaviour was to always prepend "$", which lied about
  // ^N225 (JPY), ^FTSE (GBP), ^GDAXI (EUR) etc. Now: USD-quoted
  // crypto pairs and known US indices keep "$"; non-USD indices
  // get the right symbol; unknowns drop the prefix entirely so we
  // never claim a currency we can't prove.
  const priceSym = (instrument: string, exchange: string): string => {
    if (!instrument) return "";
    const upper = instrument.toUpperCase();
    if (upper.endsWith("USDT") || upper.endsWith("USDC") || upper.endsWith("USD") ||
        upper.endsWith("-USD") || upper.endsWith("/USD")) return "$";
    if ((exchange || "").toLowerCase() === "yahoo") {
      const m: Record<string, string> = {
        "^DJI": "$", "^GSPC": "$", "^IXIC": "$", "^RUT": "$", "^VIX": "$", "^TNX": "$",
        "^FTSE": "£",
        "^GDAXI": "€", "^FCHI": "€", "^STOXX50E": "€",
        "^N225": "¥", "^TOPX": "¥",
        "^KS11": "₩",
      };
      return m[upper] ?? "";
    }
    return "";
  };

  // Format price like TUI but with the currency-aware prefix above.
  const fmtPrice = (p: number, instrument?: string, exchange?: string) => {
    const sym = priceSym(instrument ?? activeInst?.instrument ?? "", exchange ?? activeInst?.exchange ?? "");
    if (p >= 10000) return `${sym}${Math.round(p)}`;
    if (p >= 100) return `${sym}${p.toFixed(1)}`;
    if (p >= 1) return `${sym}${p.toFixed(2)}`;
    if (p >= 0.01) return `${sym}${p.toFixed(4)}`;
    return `${sym}${p.toFixed(6)}`;
  };

  return (
    <div style={s.container}>
      {/* Header — same as TUI ohlc header line */}
      <div style={s.header}>
        <span style={s.instrument}>{activeInst?.instrument || "—"}</span>
        <span style={s.exchange}>{activeInst?.exchange || ""}</span>
        <span style={s.tf}>{activeTF}</span>
        {/* Render only timeframes the source actually publishes.
            Sources like yahoo emit only 1d for indices — showing
            greyed-out 1m/5m/etc. buttons that silently fail when
            clicked was lying to the operator. Per the
            backend-resample memory the server is the right place
            to expose other TFs; until that lands we show what's
            real. */}
        {TF_ORDER
          .filter(t => t !== "spot" && availTFs.includes(t))
          .map((tf) => (
            <button
              key={tf}
              onClick={() => {
                const diff = availTFs.indexOf(tf) - availTFs.indexOf(activeTF);
                cycleTF(diff);
              }}
              style={{ ...s.tfBtn, ...(tf === activeTF ? s.tfActive : {}) }}
            >
              {tf}
            </button>
          ))}
        {availTFs.length === 1 && availTFs[0] !== "spot" && (
          <span style={s.tfHint} title={`${activeInst?.exchange || "this source"} only publishes ${availTFs[0]} for ${activeInst?.instrument || "this instrument"}`}>
            ({availTFs[0]} only)
          </span>
        )}
        {lastPrice > 0 && (
          <span style={{ ...s.price, color: change >= 0 ? colors.green : colors.red }}>
            {fmtPrice(lastPrice)}  {change >= 0 ? "+" : ""}{change.toFixed(2)}%
          </span>
        )}
        {activeInst && fetchOHLCHistoryStreaming && (() => {
          const loadKey = `${activeInst.instrument}|${activeInst.exchange}|${activeTF}`;
          const loadingChunks = ohlcLoading?.[loadKey];
          const isLoading = loadingChunks !== undefined;
          // Liquid CEX/DEX feeds backfill cleanly over multi-week
          // windows; bare 24h was a single choice and operators
          // need 7d/30d/90d for context. Less liquid sources stay
          // capped at 24h to avoid hammering rate-limited REST.
          const liquid = new Set([
            "binance", "coinbase", "kraken", "bybit", "okx",
            "mexc", "htx", "gateio", "bitget",
            "hyperliquid", "uniswap_v3",
          ]);
          const ex = (activeInst.exchange || "").toLowerCase();
          const choices = liquid.has(ex)
            ? [{ h: 24, label: "24h" }, { h: 168, label: "7d" }, { h: 720, label: "30d" }, { h: 2160, label: "90d" }]
            : [{ h: 24, label: "24h" }];
          return (
            <>
              {choices.map(({ h, label }) => (
                <button
                  key={h}
                  onClick={() => { if (!isLoading) fetchOHLCHistoryStreaming(activeInst.instrument, activeInst.exchange, activeTF, h); }}
                  disabled={isLoading}
                  title={`Load ${label} history via streaming DataRange (non-blocking)`}
                  style={{ ...s.tfBtn, ...(isLoading ? { color: colors.amber, borderColor: colors.amber } : {}) }}
                >
                  {isLoading && h === choices[0].h ? `⟳ ${loadingChunks}` : `Load ${label}`}
                </button>
              ))}
            </>
          );
        })()}
        <span style={s.count}>{candles.length} candles</span>
      </div>

      <div style={s.body}>
        {/* Chart */}
        <div ref={chartContainerRef} style={s.chart} />

        {/* Sidebar — same as TUI renderSidebar */}
        <div style={s.sidebar}>
          <div style={s.sideTitle}>INSTRUMENTS</div>
          <input ref={searchInputRef} type="text" placeholder="/ search pair/asset/exchange" value={search} onChange={(e) => setSearch(e.target.value)} onKeyDown={onSearchKey} style={s.sideSearch} />
          <div style={s.sideList}>
            {sidebarItems.map(({ key, idx, inst }) => {
              const isActive = idx === activeIdx;
              const price = inst?.timeframes.get(inst.activeTF)?.slice(-1)[0]?.close || 0;
              return (
                <div key={key} onClick={() => setActiveIdx(idx)}
                  style={{ ...s.sideItem, ...(isActive ? s.sideItemActive : {}) }}>
                  <div>
                    <span style={s.sideName}>{inst?.instrument || key}</span>
                    <span style={s.sideExch}>{inst?.exchange || ""}</span>
                  </div>
                  <span style={s.sidePrice}>{price > 0 ? fmtPrice(price, inst?.instrument, inst?.exchange) : ""}</span>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  container: { display: "flex", flexDirection: "column", height: "100%" },
  header: { display: "flex", alignItems: "center", gap: 8, padding: "6px 12px", background: "#0D0D0D", borderBottom: `1px solid ${colors.border}`, flexShrink: 0, flexWrap: "wrap" },
  instrument: { fontSize: 14, fontWeight: 900, color: colors.amber },
  exchange: { fontSize: 10, color: colors.dimText },
  tf: { display: "none" },
  tfBtn: { fontSize: 10, padding: "2px 8px", background: "none", border: `1px solid ${colors.border}`, color: colors.dimText, cursor: "pointer", borderRadius: 2, fontFamily: fonts.mono },
  tfActive: { color: colors.amber, borderColor: colors.amber, background: "#1A1200" },
  tfHint: { fontSize: 9, color: colors.dimText, marginLeft: 4, fontFamily: fonts.mono, fontStyle: "italic" },
  price: { fontSize: 13, fontWeight: 700, marginLeft: 12, fontFamily: fonts.mono },
  count: { fontSize: 10, color: colors.dimText, marginLeft: "auto", fontFamily: fonts.mono },
  body: { display: "flex", flex: 1, overflow: "hidden" },
  chart: { flex: 1 },
  sidebar: { width: 220, borderLeft: `1px solid ${colors.border}`, display: "flex", flexDirection: "column", flexShrink: 0 },
  sideTitle: { fontSize: 10, color: colors.amber, fontWeight: 700, padding: "6px 10px", letterSpacing: "0.1em" },
  sideSearch: { margin: "0 8px 4px", fontSize: 10, padding: "3px 6px", background: colors.bg, border: `1px solid ${colors.border}`, color: colors.white, borderRadius: 2, outline: "none", fontFamily: fonts.mono },
  sideList: { flex: 1, overflow: "auto" },
  sideItem: { display: "flex", justifyContent: "space-between", padding: "3px 10px", cursor: "pointer", fontSize: 11, color: colors.dimText, fontFamily: fonts.mono },
  sideItemActive: { color: colors.amber, fontWeight: 700, background: "#1A1200" },
  sideName: { },
  sideExch: { fontSize: 8, color: colors.dimText, marginLeft: 4 },
  sidePrice: { fontSize: 10 },
};
