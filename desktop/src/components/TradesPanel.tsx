import React, { useState } from "react";
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

export const TradesPanel: React.FC<Props> = ({ aggs, snaps, keys }) => {
  const [activeIdx, setActiveIdx] = useState(0);

  if (keys.length === 0) {
    return (
      <div style={s.container}>
        <div style={s.header}><span style={s.title}>TRADES</span></div>
        <div style={s.body}><span style={{ color: colors.dimText }}>Waiting for trade aggregates...</span></div>
      </div>
    );
  }

  const activeKey = keys[activeIdx] || keys[0];
  const agg = aggs[activeKey];
  const snap = snaps[activeKey];

  const buyPct = agg && agg.Volume > 0 ? (agg.BuyVolume / agg.Volume * 100) : 0;

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.title}>TRADES</span>
        {/* Instrument tabs */}
        {keys.map((k, i) => (
          <span
            key={k}
            onClick={() => setActiveIdx(i)}
            style={{
              ...s.tab,
              color: i === activeIdx ? colors.amber : colors.dimText,
              borderBottom: i === activeIdx ? `2px solid ${colors.amber}` : "2px solid transparent",
              cursor: "pointer",
            }}
          >
            {k.split("/")[1] || k}
          </span>
        ))}
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
  );
};

const s: Record<string, React.CSSProperties> = {
  container: { display: "flex", flexDirection: "column", height: "100%" },
  header: {
    display: "flex", alignItems: "center", gap: 16,
    padding: "6px 12px", background: "#0D0D0D",
    borderBottom: `1px solid ${colors.border}`, flexShrink: 0,
  },
  title: { fontSize: 13, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  tab: { fontSize: 11, fontWeight: 700, fontFamily: fonts.mono, padding: "2px 8px" },
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
