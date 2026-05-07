import React, { useState, useMemo, useEffect, useRef } from "react";
import { colors, fonts, tableCellStyle, tableHeaderStyle } from "../styles/theme";
import { TradeAgg, TradeSnapData } from "../store";

interface Props {
  aggs: Record<string, TradeAgg>;
  snaps: Record<string, TradeSnapData>;
  keys: string[];
}

function fmt(v: number, dp: number = 2): string {
  return v.toFixed(dp);
}

function fmtLarge(v: number): string {
  if (v >= 1e9) return `${(v / 1e9).toFixed(2)}B`;
  if (v >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
  if (v >= 1e3) return `${(v / 1e3).toFixed(1)}K`;
  return v.toFixed(2);
}

// Server keys are "<exchange>/<instrument>"; surface both in the
// sidebar so 3x BTCUSDT (binance / bybit / bitget) is no longer
// indistinguishable.
function splitKey(k: string): { exchange: string; instrument: string } {
  const i = k.indexOf("/");
  if (i < 0) return { exchange: "", instrument: k };
  return { exchange: k.slice(0, i), instrument: k.slice(i + 1) };
}

export const TradesPanel: React.FC<Props> = ({ aggs, snaps, keys }) => {
  const [activeIdx, setActiveIdx] = useState(0);
  const [search, setSearch] = useState("");
  const searchInputRef = useRef<HTMLInputElement>(null);
  const activeRowRef = useRef<HTMLDivElement>(null);

  // Clamp activeIdx if keys shrink — avoid landing on a deleted instrument.
  useEffect(() => {
    if (activeIdx >= keys.length) setActiveIdx(0);
  }, [keys.length, activeIdx]);

  // Keyboard nav — match OHLC: '[' / ']' / ArrowLeft / ArrowRight
  // step the active instrument; '/' focuses the sidebar search.
  // Skip when typing in an input so the search field stays usable.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      if (keys.length === 0) return;
      if (e.key === "[" || e.key === "ArrowLeft") {
        e.preventDefault();
        setActiveIdx((i) => (i - 1 + keys.length) % keys.length);
      } else if (e.key === "]" || e.key === "ArrowRight") {
        e.preventDefault();
        setActiveIdx((i) => (i + 1) % keys.length);
      } else if (e.key === "/") {
        e.preventDefault();
        searchInputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [keys.length]);

  // Scroll the selected sidebar row into view when activeIdx
  // changes via keyboard so the user always sees the cursor.
  useEffect(() => {
    activeRowRef.current?.scrollIntoView({ block: "nearest" });
  }, [activeIdx]);

  const sidebarItems = useMemo(() => {
    const q = search.toLowerCase();
    return keys
      .map((key, idx) => ({ key, idx, ...splitKey(key) }))
      .filter(({ key, exchange, instrument }) =>
        !q
        || instrument.toLowerCase().includes(q)
        || exchange.toLowerCase().includes(q)
        || key.toLowerCase().includes(q))
      .sort((a, b) =>
        a.instrument.localeCompare(b.instrument) ||
        a.exchange.localeCompare(b.exchange)
      );
  }, [keys, search]);

  if (keys.length === 0) {
    return (
      <div style={s.container}>
        <div style={s.header}><span style={s.title}>TRADES</span></div>
        <div style={s.body}><span style={{ color: colors.dimText }}>Waiting for trade aggregates...</span></div>
      </div>
    );
  }

  const activeKey = keys[activeIdx] || keys[0];
  const { exchange: actEx, instrument: actInst } = splitKey(activeKey);
  const agg = aggs[activeKey];
  const snap = snaps[activeKey];

  const buyPct = agg && agg.Volume > 0 ? (agg.BuyVolume / agg.Volume * 100) : 0;

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.title}>TRADES</span>
        <span style={s.activeName}>{actInst}</span>
        <span style={s.activeExch}>{actEx}</span>
        <span style={s.hint}>[/]:pair  /:search</span>
      </div>

      <div style={s.split}>
        {/* Sidebar — searchable instrument list, scroll-into-view on active. */}
        <div style={s.sidebar}>
          <div style={s.sideTitle}>INSTRUMENTS</div>
          <input
            ref={searchInputRef}
            type="text"
            placeholder="/ search pair/exchange"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                setSearch("");
                searchInputRef.current?.blur();
              } else if (e.key === "Enter") {
                const first = sidebarItems[0];
                if (first) setActiveIdx(first.idx);
                searchInputRef.current?.blur();
              }
            }}
            style={s.sideSearch}
          />
          <div style={s.sideList}>
            {sidebarItems.map(({ key, idx, exchange, instrument }) => {
              const isActive = idx === activeIdx;
              const a = aggs[key];
              const last = a ? a.Close : 0;
              return (
                <div
                  key={key}
                  ref={isActive ? activeRowRef : undefined}
                  onClick={() => setActiveIdx(idx)}
                  style={{ ...s.sideItem, ...(isActive ? s.sideItemActive : {}) }}
                >
                  <div>
                    <span style={s.sideName}>{instrument}</span>
                    <span style={s.sideExch}>{exchange}</span>
                  </div>
                  <span style={s.sidePrice}>{last > 0 ? fmt(last) : ""}</span>
                </div>
              );
            })}
          </div>
        </div>

        <div style={s.body}>
        {/* Aggregate stats */}
        {agg && (
          <div style={s.statsGrid}>
            <div style={s.statBox}>
              <div style={s.statLabel}>VWAP</div>
              <div style={{ ...s.statValue, color: colors.cyan }}>{fmt(agg.VWAP)}</div>
            </div>
            <div style={s.statBox}>
              <div style={s.statLabel}>Volume</div>
              <div style={s.statValue}>{fmt(agg.Volume, 4)}</div>
            </div>
            <div style={s.statBox}>
              <div style={s.statLabel}>Buy / Sell</div>
              <div style={s.statValue}>
                <span style={{ color: colors.green }}>{fmt(agg.BuyVolume, 4)}</span>
                {" / "}
                <span style={{ color: colors.red }}>{fmt(agg.SellVolume, 4)}</span>
                <span style={{ color: colors.dimText }}> ({buyPct.toFixed(0)}%)</span>
              </div>
            </div>
            <div style={s.statBox}>
              <div style={s.statLabel}>High / Low</div>
              <div style={s.statValue}>
                <span style={{ color: colors.green }}>{fmt(agg.High)}</span>
                {" / "}
                <span style={{ color: colors.red }}>{fmt(agg.Low)}</span>
              </div>
            </div>
            <div style={s.statBox}>
              <div style={s.statLabel}>Trades/s</div>
              <div style={s.statValue}>{agg.Count}</div>
            </div>
            <div style={s.statBox}>
              <div style={s.statLabel}>Turnover</div>
              <div style={s.statValue}>${fmtLarge(agg.Turnover)}</div>
            </div>
            <div style={s.statBox}>
              <div style={s.statLabel}>Quantiles</div>
              <div style={{ ...s.statValue, fontSize: 11 }}>
                P25: {fmt(agg.P25)} | P50: {fmt(agg.P50)} | P75: {fmt(agg.P75)}
              </div>
            </div>
          </div>
        )}

        {/* Recent trades table */}
        {snap && snap.Trades.length > 0 && (
          <div style={{ marginTop: 12 }}>
            <div style={{ ...s.statLabel, marginBottom: 4 }}>RECENT TRADES</div>
            <table style={{ borderCollapse: "collapse", width: "100%" }}>
              <thead>
                <tr>
                  <th style={tableHeaderStyle}>Side</th>
                  <th style={tableHeaderStyle}>Price</th>
                  <th style={tableHeaderStyle}>Qty</th>
                  <th style={tableHeaderStyle}>Time</th>
                </tr>
              </thead>
              <tbody>
                {[...snap.Trades].reverse().map((t, i) => {
                  const sideColor = t.Side === "buy" ? colors.green : colors.red;
                  const ts = t.Timestamp ? new Date(t.Timestamp).toLocaleTimeString() : "";
                  return (
                    <tr key={i}>
                      <td style={{ ...tableCellStyle, color: sideColor, fontWeight: 700 }}>
                        {t.Side.toUpperCase()}
                      </td>
                      <td style={tableCellStyle}>{fmt(t.Price)}</td>
                      <td style={tableCellStyle}>{fmt(t.Quantity, 6)}</td>
                      <td style={{ ...tableCellStyle, color: colors.dimText }}>{ts}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        </div>
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  container: { display: "flex", flexDirection: "column", height: "100%" },
  header: {
    display: "flex", alignItems: "center", gap: 12,
    padding: "6px 12px", background: "#0D0D0D",
    borderBottom: `1px solid ${colors.border}`, flexShrink: 0,
  },
  title: { fontSize: 13, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  activeName: { fontSize: 14, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  activeExch: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono },
  hint: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono, marginLeft: "auto" },
  split: { display: "flex", flex: 1, overflow: "hidden" },
  sidebar: { width: 220, borderRight: `1px solid ${colors.border}`, display: "flex", flexDirection: "column", flexShrink: 0 },
  sideTitle: { fontSize: 10, color: colors.amber, fontWeight: 700, padding: "6px 10px", letterSpacing: "0.1em", fontFamily: fonts.mono },
  sideSearch: { margin: "0 8px 4px", fontSize: 10, padding: "3px 6px", background: "#0a0a0a", border: `1px solid ${colors.border}`, color: colors.white, borderRadius: 2, outline: "none", fontFamily: fonts.mono },
  sideList: { flex: 1, overflow: "auto" },
  sideItem: { display: "flex", justifyContent: "space-between", padding: "3px 10px", cursor: "pointer", fontSize: 11, color: colors.dimText, fontFamily: fonts.mono },
  sideItemActive: { color: colors.amber, fontWeight: 700, background: "#1A1200" },
  sideName: {},
  sideExch: { fontSize: 8, color: colors.dimText, marginLeft: 4 },
  sidePrice: { fontSize: 10 },
  body: { flex: 1, overflow: "auto", padding: 12 },
  statsGrid: {
    display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(180px, 1fr))",
    gap: 8,
  },
  statBox: {
    background: "#111", border: `1px solid ${colors.border}`, borderRadius: 4,
    padding: "8px 12px",
  },
  statLabel: {
    fontSize: 10, fontWeight: 700, color: colors.dimText, fontFamily: fonts.mono,
    textTransform: "uppercase" as const, letterSpacing: "0.05em", marginBottom: 2,
  },
  statValue: { fontSize: 14, fontWeight: 700, color: colors.white, fontFamily: fonts.mono },
};
