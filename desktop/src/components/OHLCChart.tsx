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
}

const TF_ORDER = ["1m", "5m", "15m", "1h", "4h", "1d", "spot"];

export const OHLCChart: React.FC<Props> = ({ ohlcData, ohlcKeys, activeIdx, setActiveIdx, cycleTF, fetchOHLCHistory, fetchOHLCHistoryStreaming, ohlcLoading }) => {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
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

  // Sorted + filtered sidebar items.
  const sidebarItems = useMemo(() => {
    const q = search.toLowerCase();
    return ohlcKeys
      .map((key, idx) => ({ key, idx, inst: ohlcData.get(key) }))
      .filter(({ inst }) => !q || (inst?.instrument || "").toLowerCase().includes(q) || (inst?.exchange || "").toLowerCase().includes(q))
      .sort((a, b) => (a.inst?.instrument || "").localeCompare(b.inst?.instrument || ""));
  }, [ohlcKeys, ohlcData, search]);

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

  // Update chart data — re-render on candles OR timeframe change.
  useEffect(() => {
    if (!seriesRef.current || !chartRef.current) return;
    try {
      if (candles.length === 0) {
        seriesRef.current.setData([]);
        return;
      }
      // Dedup by time, sort ascending — lightweight-charts requires this.
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
    } catch (e) { console.debug("[chart] update error:", e); }
  }, [candles, activeTF, activeKey]);

  // Format price like TUI.
  const fmtPrice = (p: number) => {
    if (p >= 10000) return `$${Math.round(p)}`;
    if (p >= 100) return `$${p.toFixed(1)}`;
    if (p >= 1) return `$${p.toFixed(2)}`;
    if (p >= 0.01) return `$${p.toFixed(4)}`;
    return `$${p.toFixed(6)}`;
  };

  return (
    <div style={s.container}>
      {/* Header — same as TUI ohlc header line */}
      <div style={s.header}>
        <span style={s.instrument}>{activeInst?.instrument || "—"}</span>
        <span style={s.exchange}>{activeInst?.exchange || ""}</span>
        <span style={s.tf}>{activeTF}</span>
        {TF_ORDER.filter(t => t !== "spot").map((tf) => {
          const hasData = availTFs.includes(tf);
          return (
            <button key={tf} onClick={() => {
              if (hasData) {
                const diff = availTFs.indexOf(tf) - availTFs.indexOf(activeTF);
                cycleTF(diff);
              } else if (activeInst && fetchOHLCHistory) {
                // Fetch from server cache for this TF.
                fetchOHLCHistory(activeInst.instrument, activeInst.exchange, tf);
              }
            }}
              style={{ ...s.tfBtn, ...(tf === activeTF ? s.tfActive : {}), ...(hasData ? {} : { opacity: 0.3, cursor: "pointer" }) }}>{tf}</button>
          );
        })}
        {lastPrice > 0 && (
          <span style={{ ...s.price, color: change >= 0 ? colors.green : colors.red }}>
            {fmtPrice(lastPrice)}  {change >= 0 ? "+" : ""}{change.toFixed(2)}%
          </span>
        )}
        {activeInst && fetchOHLCHistoryStreaming && (() => {
          const loadKey = `${activeInst.instrument}|${activeInst.exchange}|${activeTF}`;
          const loadingChunks = ohlcLoading?.[loadKey];
          const isLoading = loadingChunks !== undefined;
          return (
            <button
              onClick={() => { if (!isLoading) fetchOHLCHistoryStreaming(activeInst.instrument, activeInst.exchange, activeTF, 24); }}
              disabled={isLoading}
              title="Load 24h history via streaming DataRange (non-blocking)"
              style={{ ...s.tfBtn, ...(isLoading ? { color: colors.amber, borderColor: colors.amber } : {}) }}
            >
              {isLoading ? `⟳ ${loadingChunks}` : "Load 24h"}
            </button>
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
          <input type="text" placeholder="/ search..." value={search} onChange={(e) => setSearch(e.target.value)} style={s.sideSearch} />
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
                  <span style={s.sidePrice}>{price > 0 ? fmtPrice(price) : ""}</span>
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
