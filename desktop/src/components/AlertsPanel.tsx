import React from "react";
import { colors, fonts, panelStyle } from "../styles/theme";
import type { AlertEntry } from "../store";

interface Props { items: AlertEntry[]; }

export const AlertsPanel: React.FC<Props> = ({ items }) => (
  <div style={{ ...panelStyle, display: "flex", flexDirection: "column", padding: 0 }}>
    <div style={styles.header}>
      <span style={styles.label}>ALERTS</span>
      <span style={styles.count}>{items.length}</span>
    </div>
    <div style={styles.list}>
      {items.length === 0 && <div style={styles.empty}>No alerts. Create from TUI: /ALERT SET BTCUSDT &gt; 100000</div>}
      {items.map((a) => (
        <div key={a.id} style={styles.row}>
          <span style={{ ...styles.status, color: a.status === "active" ? colors.green : colors.amber }}>{a.status}</span>
          <span style={styles.type}>{a.type}</span>
          <span style={styles.instrument}>{a.instrument}</span>
          <span style={styles.time}>{new Date(a.timestamp * 1000).toLocaleTimeString()}</span>
        </div>
      ))}
    </div>
  </div>
);

const styles: Record<string, React.CSSProperties> = {
  header: { display: "flex", alignItems: "center", gap: 12, padding: "8px 12px", borderBottom: `1px solid ${colors.border}`, background: colors.surface, flexShrink: 0 },
  label: { fontFamily: fonts.mono, fontSize: 13, fontWeight: 900, color: colors.amber },
  count: { fontFamily: fonts.mono, fontSize: 10, color: colors.dimText },
  list: { flex: 1, overflow: "auto" },
  empty: { padding: 16, color: colors.dimText, fontFamily: fonts.mono, fontSize: 12 },
  row: { display: "flex", gap: 12, padding: "5px 12px", borderBottom: `1px solid ${colors.border}`, fontFamily: fonts.mono, fontSize: 11 },
  status: { fontWeight: 700, minWidth: 70 },
  type: { color: colors.dimText, minWidth: 70 },
  instrument: { color: colors.white },
  time: { color: colors.dimText, marginLeft: "auto" },
};
