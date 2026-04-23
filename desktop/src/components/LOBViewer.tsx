import React, { useState, useMemo } from "react";
import { colors, fonts } from "../styles/theme";
import type { LOBData } from "../store";

interface Props {
  lobData: Map<string, LOBData>;
  lobKeys: string[];
  activeIdx: number;
  setActiveIdx: (idx: number) => void;
}

function formatAge(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return "—";
  if (seconds < 60) return `${Math.floor(seconds)}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  return `${Math.floor(seconds / 3600)}h ago`;
}

export const LOBViewer: React.FC<Props> = ({ lobData, lobKeys, activeIdx, setActiveIdx }) => {
  const [search, setSearch] = useState("");
  const activeKey = lobKeys[activeIdx] || "";
  const snap = lobData.get(activeKey);

  const sidebarItems = useMemo(() => {
    const q = search.toLowerCase();
    return lobKeys
      .map((key, idx) => ({ key, idx, d: lobData.get(key) }))
      .filter(({ d }) => !q || (d?.instrument || "").toLowerCase().includes(q) || (d?.exchange || "").toLowerCase().includes(q))
      .sort((a, b) => (a.d?.instrument || "").localeCompare(b.d?.instrument || ""));
  }, [lobKeys, lobData, search]);

  const maxQty = snap ? Math.max(...snap.bids.map((l) => l.qty), ...snap.asks.map((l) => l.qty), 0.001) : 1;
  const spread = snap && snap.bids.length > 0 && snap.asks.length > 0 ? snap.asks[0].price - snap.bids[0].price : 0;

  return (
    <div style={s.container}>
      {/* Header */}
      <div style={s.header}>
        <span style={s.instrument}>{snap?.instrument || "—"}</span>
        {snap?.exchange && (
          <span style={s.exchangeBadge}>{snap.exchange.toUpperCase()}</span>
        )}
        <span style={s.spread}>Spread: ${spread.toFixed(2)}</span>
        {snap?.lastUpdate && (
          <span style={s.freshness} title="Last update">
            {formatAge(Date.now() / 1000 - snap.lastUpdate)}
          </span>
        )}
      </div>

      <div style={s.body}>
        {/* Book */}
        <div style={s.book}>
          {!snap || snap.bids.length === 0 ? (
            <div style={s.empty}>Waiting for order book data...</div>
          ) : (
            <>
              <div style={s.colHeaders}>
                <span style={{ ...s.colH, textAlign: "right" }}>QTY</span>
                <span style={{ ...s.colH, textAlign: "right" }}>BID</span>
                <span style={s.colH}>ASK</span>
                <span style={s.colH}>QTY</span>
              </div>
              <div style={s.rows}>
                {Array.from({ length: Math.max(snap.bids.length, snap.asks.length) }).map((_, i) => {
                  const bid = snap.bids[i];
                  const ask = snap.asks[i];
                  return (
                    <div key={i} style={s.row}>
                      <div style={s.bidSide}>
                        {bid && <>
                          <div style={{ ...s.bar, right: 0, width: `${(bid.qty / maxQty) * 100}%`, background: "rgba(0,255,0,0.15)" }} />
                          <span style={{ ...s.cell, color: colors.green }}>{bid.qty.toFixed(4)}</span>
                          <span style={{ ...s.cell, color: colors.green, fontWeight: 600 }}>{bid.price.toFixed(2)}</span>
                        </>}
                      </div>
                      <div style={s.askSide}>
                        {ask && <>
                          <div style={{ ...s.bar, left: 0, width: `${(ask.qty / maxQty) * 100}%`, background: "rgba(255,68,68,0.15)" }} />
                          <span style={{ ...s.cell, color: colors.red, fontWeight: 600 }}>{ask.price.toFixed(2)}</span>
                          <span style={{ ...s.cell, color: colors.red }}>{ask.qty.toFixed(4)}</span>
                        </>}
                      </div>
                    </div>
                  );
                })}
              </div>
            </>
          )}
        </div>

        {/* Sidebar */}
        <div style={s.sidebar}>
          <div style={s.sideTitle}>ORDER BOOKS</div>
          <input type="text" placeholder="/ search..." value={search} onChange={(e) => setSearch(e.target.value)} style={s.sideSearch} />
          <div style={s.sideList}>
            {sidebarItems.map(({ key, idx, d }) => {
              const isActive = idx === activeIdx;
              const sp = d && d.bids.length > 0 && d.asks.length > 0 ? (d.asks[0].price - d.bids[0].price).toFixed(2) : "—";
              return (
                <div key={key} onClick={() => setActiveIdx(idx)}
                  style={{ ...s.sideItem, ...(isActive ? s.sideItemActive : {}) }}>
                  <div>
                    <span>{d?.instrument || key}</span>
                    <span style={s.sideExch}>{d?.exchange || ""}</span>
                  </div>
                  <span style={s.sideSpread}>sp ${sp}</span>
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
  header: { display: "flex", alignItems: "center", gap: 10, padding: "6px 12px", background: "#0D0D0D", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 },
  instrument: { fontSize: 14, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  exchange: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono },
  exchangeBadge: {
    fontSize: 10,
    fontWeight: 800,
    color: colors.bg,
    background: colors.amber,
    padding: "2px 8px",
    borderRadius: 3,
    letterSpacing: "0.08em",
    fontFamily: fonts.mono,
  },
  spread: { fontSize: 10, color: colors.amber, marginLeft: "auto", fontFamily: fonts.mono },
  freshness: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono },
  body: { display: "flex", flex: 1, overflow: "hidden" },
  book: { flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" },
  empty: { padding: 20, color: colors.dimText, fontFamily: fonts.mono, fontSize: 12 },
  colHeaders: { display: "flex", padding: "4px 12px", borderBottom: `1px solid ${colors.border}`, fontFamily: fonts.mono, fontSize: 9, color: colors.dimText, fontWeight: 700, gap: 0 },
  colH: { flex: 1, letterSpacing: "0.08em" },
  rows: { flex: 1, overflow: "auto" },
  row: { display: "flex", fontFamily: fonts.mono, fontSize: 11 },
  bidSide: { flex: 1, display: "flex", justifyContent: "flex-end", position: "relative", padding: "2px 4px 2px 12px" },
  askSide: { flex: 1, display: "flex", position: "relative", padding: "2px 12px 2px 4px" },
  bar: { position: "absolute", top: 0, bottom: 0, zIndex: 0 },
  cell: { position: "relative", zIndex: 1, minWidth: 80, textAlign: "right", padding: "0 4px" },
  sidebar: { width: 220, borderLeft: `1px solid ${colors.border}`, display: "flex", flexDirection: "column", flexShrink: 0 },
  sideTitle: { fontSize: 10, color: colors.amber, fontWeight: 700, padding: "6px 10px", letterSpacing: "0.1em", fontFamily: fonts.mono },
  sideSearch: { margin: "0 8px 4px", fontSize: 10, padding: "3px 6px", background: colors.bg, border: `1px solid ${colors.border}`, color: colors.white, borderRadius: 2, outline: "none", fontFamily: fonts.mono },
  sideList: { flex: 1, overflow: "auto" },
  sideItem: { display: "flex", justifyContent: "space-between", padding: "3px 10px", cursor: "pointer", fontSize: 11, color: colors.dimText, fontFamily: fonts.mono },
  sideItemActive: { color: colors.amber, fontWeight: 700, background: "#1A1200" },
  sideExch: { fontSize: 8, color: colors.dimText, marginLeft: 4 },
  sideSpread: { fontSize: 9, color: colors.dimText },
};
