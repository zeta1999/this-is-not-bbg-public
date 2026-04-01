import React from "react";
import { colors, fonts, panelStyle } from "../styles/theme";
import type { FeedStatus } from "../store";

interface Props { feeds: FeedStatus[]; }

export const MonitorPanel: React.FC<Props> = ({ feeds }) => (
  <div style={{ ...panelStyle, display: "flex", flexDirection: "column", padding: 0 }}>
    <div style={styles.header}>
      <span style={styles.label}>FEED MONITOR</span>
      <span style={styles.count}>{feeds.length} feeds</span>
    </div>
    <div style={styles.colHeaders}>
      <span style={{ ...styles.col, minWidth: 180 }}>SOURCE</span>
      <span style={{ ...styles.col, minWidth: 100 }}>STATUS</span>
      <span style={{ ...styles.col, minWidth: 80 }}>LATENCY</span>
      <span style={{ ...styles.col, minWidth: 60 }}>ERRORS</span>
    </div>
    <div style={styles.list}>
      {feeds.length === 0 && <div style={styles.empty}>Waiting for feed statuses...</div>}
      {feeds.map((f) => {
        const c = f.state === "connected" ? colors.green : f.state === "error" ? colors.red : colors.amber;
        return (
          <div key={f.name} style={styles.row}>
            <span style={{ ...styles.col, minWidth: 180, color: colors.white }}>
              <span style={{ ...styles.dot, background: c }} />{f.name}
            </span>
            <span style={{ ...styles.col, minWidth: 100, color: c }}>{f.state}</span>
            <span style={{ ...styles.col, minWidth: 80, color: colors.dimText }}>{f.latencyMs.toFixed(1)}ms</span>
            <span style={{ ...styles.col, minWidth: 60, color: f.errorCount > 0 ? colors.red : colors.dimText }}>{f.errorCount}</span>
          </div>
        );
      })}
    </div>
  </div>
);

const styles: Record<string, React.CSSProperties> = {
  header: { display: "flex", alignItems: "center", gap: 12, padding: "8px 12px", borderBottom: `1px solid ${colors.border}`, background: colors.surface, flexShrink: 0 },
  label: { fontFamily: fonts.mono, fontSize: 13, fontWeight: 900, color: colors.amber },
  count: { fontFamily: fonts.mono, fontSize: 10, color: colors.dimText },
  colHeaders: { display: "flex", gap: 8, padding: "4px 12px", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 },
  col: { fontFamily: fonts.mono, fontSize: 10, fontWeight: 700, color: colors.dimText },
  list: { flex: 1, overflow: "auto" },
  empty: { padding: 16, color: colors.dimText, fontFamily: fonts.mono, fontSize: 12 },
  row: { display: "flex", gap: 8, padding: "4px 12px", borderBottom: `1px solid ${colors.border}`, fontFamily: fonts.mono, fontSize: 11, alignItems: "center" },
  dot: { display: "inline-block", width: 7, height: 7, borderRadius: "50%", marginRight: 6 },
};
