import React, { useMemo } from "react";
import { colors, fonts, panelStyle } from "../styles/theme";
import type { SanitySnapshot } from "../store";

interface Props {
  snapshots: Record<string, SanitySnapshot>;
}

// SanityPanel renders one card per configured pair, showing the
// cross-venue median + each venue's mid with % delta + stale/outlier
// flags. Driven by `sanity.prices` SSE messages — zero local polling.
//
// NO FREEZE alignment: when the server has no snapshots yet (because
// no venues have published prices), we render a waiting-state card
// rather than blocking or hiding — the whole point of this view is
// to show "what's the state of the world right now", even when that
// state is "nothing has arrived yet".
export const SanityPanel: React.FC<Props> = ({ snapshots }) => {
  const rows = useMemo(() => {
    return Object.values(snapshots).sort((a, b) =>
      a.instrument.localeCompare(b.instrument)
    );
  }, [snapshots]);

  return (
    <div style={{ ...panelStyle, display: "flex", flexDirection: "column", padding: 0 }}>
      <div style={styles.header}>
        <span style={styles.label}>CROSS-VENUE SANITY</span>
        <span style={styles.hint}>
          median of mids · flags any venue &gt; threshold away or stale
          &gt; 5m
        </span>
        <span style={styles.count}>{rows.length} pairs</span>
      </div>

      {rows.length === 0 && (
        <div style={styles.empty}>
          Waiting for sanity.prices — configure <code>alerts.sanity_pairs</code> on
          the server and ensure at least one venue is publishing OHLC/LOB.
        </div>
      )}

      <div style={styles.grid}>
        {rows.map((snap) => {
          const ageSec = (Date.now() - snap.lastUpdate) / 1000;
          const headerStale = ageSec > 60;
          return (
            <div key={snap.instrument} style={styles.card}>
              <div style={{ ...styles.cardHeader, ...(snap.outlier_count > 0 ? styles.cardHeaderAlert : {}) }}>
                <span style={styles.instrument}>{snap.instrument}</span>
                <span style={styles.median}>median {fmtPrice(snap.median)}</span>
                {snap.outlier_count > 0 && (
                  <span style={styles.outlierBadge}>
                    {snap.outlier_count} outlier{snap.outlier_count === 1 ? "" : "s"}
                  </span>
                )}
                <span style={{ ...styles.age, color: headerStale ? colors.red : colors.dimText }}>
                  {fmtAge(ageSec)} ago
                </span>
              </div>
              <div style={styles.venueList}>
                {snap.venues.length === 0 && (
                  <div style={styles.empty}>no venues with fresh data</div>
                )}
                {snap.venues.map((v) => {
                  const deltaColor =
                    Math.abs(v.delta_pct) > snap.threshold_pct ? colors.red : colors.dimText;
                  return (
                    <div
                      key={v.exchange}
                      style={{ ...styles.venue, ...(v.outlier ? styles.venueAlert : {}) }}
                    >
                      <span style={styles.venueName}>{v.exchange}</span>
                      <span style={styles.venuePrice}>{fmtPrice(v.mid)}</span>
                      <span style={{ ...styles.venueDelta, color: deltaColor }}>
                        {v.delta_pct >= 0 ? "+" : ""}
                        {v.delta_pct.toFixed(3)}%
                      </span>
                      <span style={styles.venueAge}>{fmtAge(v.age_seconds)}</span>
                      {v.outlier && <span style={styles.flagBadge}>FLAG</span>}
                    </div>
                  );
                })}
              </div>
              <div style={styles.cardFooter}>
                threshold ±{snap.threshold_pct.toFixed(2)}% · {snap.venue_count} venue
                {snap.venue_count === 1 ? "" : "s"}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

function fmtPrice(p: number): string {
  if (!p) return "—";
  if (p >= 10000) return `$${Math.round(p).toLocaleString()}`;
  if (p >= 100) return `$${p.toFixed(1)}`;
  if (p >= 1) return `$${p.toFixed(2)}`;
  if (p >= 0.01) return `$${p.toFixed(4)}`;
  return `$${p.toFixed(6)}`;
}

function fmtAge(sec: number): string {
  if (sec < 60) return `${Math.max(0, Math.floor(sec))}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m`;
  return `${Math.floor(sec / 3600)}h`;
}

const styles: Record<string, React.CSSProperties> = {
  header: {
    display: "flex",
    alignItems: "center",
    gap: 12,
    padding: "8px 12px",
    borderBottom: `1px solid ${colors.border}`,
    background: colors.surface,
    flexShrink: 0,
    flexWrap: "wrap",
  },
  label: { fontFamily: fonts.mono, fontSize: 13, fontWeight: 900, color: colors.amber },
  hint: { fontFamily: fonts.mono, fontSize: 10, color: colors.dimText },
  count: { fontFamily: fonts.mono, fontSize: 10, color: colors.dimText, marginLeft: "auto" },
  empty: { padding: 16, color: colors.dimText, fontFamily: fonts.mono, fontSize: 12 },
  grid: {
    flex: 1,
    overflow: "auto",
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(360px, 1fr))",
    gap: 10,
    padding: 10,
    alignContent: "start",
  },
  card: {
    background: "#0D0D0D",
    border: `1px solid ${colors.border}`,
    borderRadius: 3,
    display: "flex",
    flexDirection: "column",
    fontFamily: fonts.mono,
  },
  cardHeader: {
    display: "flex",
    alignItems: "center",
    gap: 8,
    padding: "6px 10px",
    borderBottom: `1px solid ${colors.border}`,
    flexWrap: "wrap",
  },
  cardHeaderAlert: { background: "#2A0A0A", borderBottomColor: colors.red },
  instrument: { fontSize: 12, fontWeight: 900, color: colors.amber },
  median: { fontSize: 11, color: colors.white },
  outlierBadge: {
    fontSize: 9,
    fontWeight: 700,
    color: colors.bg,
    background: colors.red,
    padding: "1px 5px",
    borderRadius: 2,
    letterSpacing: "0.05em",
  },
  age: { fontSize: 10, marginLeft: "auto" },
  venueList: { display: "flex", flexDirection: "column", padding: "4px 0" },
  venue: {
    display: "grid",
    gridTemplateColumns: "1fr auto auto auto auto",
    gap: 10,
    alignItems: "center",
    padding: "3px 10px",
    fontSize: 11,
    color: colors.dimText,
  },
  venueAlert: { background: "#1A0A0A", color: colors.white },
  venueName: { fontWeight: 700, color: "#4488FF" },
  venuePrice: { fontFamily: fonts.mono, fontSize: 11 },
  venueDelta: { fontSize: 10, minWidth: 60, textAlign: "right" },
  venueAge: { fontSize: 9, color: colors.dimText, minWidth: 30, textAlign: "right" },
  flagBadge: {
    fontSize: 8,
    fontWeight: 700,
    color: colors.bg,
    background: colors.red,
    padding: "1px 4px",
    borderRadius: 2,
  },
  cardFooter: {
    padding: "4px 10px",
    borderTop: `1px solid ${colors.border}`,
    fontSize: 9,
    color: colors.dimText,
  },
};
