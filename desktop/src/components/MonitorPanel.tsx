import React from "react";
import { colors, fonts, panelStyle } from "../styles/theme";
import type { FeedStatus, PluginStatus, BusStats, WALStats } from "../store";

interface Props {
  feeds: FeedStatus[];
  plugins: PluginStatus[];
  busStats?: BusStats;
  wals?: WALStats[];
}

function humanBytes(n: number): string {
  if (!isFinite(n) || n <= 0) return "0B";
  const units = ["B", "K", "M", "G", "T"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i === 0 ? `${v.toFixed(0)}${units[i]}` : `${v.toFixed(1)}${units[i]}`;
}

function ago(tsMs: number): string {
  if (!tsMs) return "—";
  const d = Date.now() - tsMs;
  if (d < 1000) return "just now";
  if (d < 60_000) return `${Math.floor(d / 1000)}s ago`;
  if (d < 3_600_000) return `${Math.floor(d / 60_000)}m ago`;
  return `${Math.floor(d / 3_600_000)}h ago`;
}

function dotColor(state: string): string {
  if (state === "connected") return colors.green;
  if (state === "error") return colors.red;
  if (state === "stopped") return colors.dimText;
  return colors.amber; // stale / reconnecting / unknown
}

export const MonitorPanel: React.FC<Props> = ({ feeds, plugins, busStats, wals = [] }) => (
  <div style={{ ...panelStyle, display: "flex", flexDirection: "column", padding: 0 }}>
    {(busStats || wals.length > 0) && (
      <>
        <div style={styles.header}>
          <span style={styles.label}>BACKPRESSURE</span>
        </div>
        {busStats && (
          <div style={styles.row}>
            <span style={{ ...styles.col, color: colors.green, minWidth: 60 }}>bus</span>
            <span style={{ ...styles.col, minWidth: 90, color: colors.dimText }}>
              subs={busStats.subscribers}
            </span>
            <span style={{ ...styles.col, minWidth: 100, color: colors.dimText }}>
              topics={busStats.topics}
            </span>
            <span style={{ ...styles.col, minWidth: 120, color: busStats.dropped > 0 ? colors.red : colors.dimText }}>
              dropped={busStats.dropped}
            </span>
            <span style={{ ...styles.col, minWidth: 80, color: colors.dimText }}>
              {ago(busStats.tsMs)}
            </span>
          </div>
        )}
        {wals.map((w) => {
          const dropped = w.busDropped + w.walDropped;
          return (
            <div key={w.name} style={styles.row}>
              <span style={{ ...styles.col, color: colors.green, minWidth: 80 }}>wal.{w.name}</span>
              <span style={{ ...styles.col, minWidth: 110, color: dropped > 0 ? colors.red : colors.dimText }}>
                bus_drop={w.busDropped}
              </span>
              <span style={{ ...styles.col, minWidth: 110, color: dropped > 0 ? colors.red : colors.dimText }}>
                wal_drop={w.walDropped}
              </span>
              <span style={{ ...styles.col, minWidth: 110, color: colors.dimText }}>
                bytes={humanBytes(w.bytesOnDisk)}
              </span>
              <span style={{ ...styles.col, minWidth: 80, color: colors.dimText }}>
                segs={w.segments}
              </span>
              <span style={{ ...styles.col, minWidth: 80, color: colors.dimText }}>
                {ago(w.tsMs)}
              </span>
            </div>
          );
        })}
      </>
    )}
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
    <div style={styles.feedList}>
      {feeds.length === 0 && <div style={styles.empty}>Waiting for feed statuses...</div>}
      {feeds.map((f) => {
        const c = dotColor(f.state);
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

    <div style={styles.header}>
      <span style={styles.label}>PLUGIN MONITOR</span>
      <span style={styles.count}>{plugins.length} plugins</span>
    </div>
    <div style={styles.colHeaders}>
      <span style={{ ...styles.col, minWidth: 180 }}>PLUGIN</span>
      <span style={{ ...styles.col, minWidth: 100 }}>STATUS</span>
      <span style={{ ...styles.col, minWidth: 60 }}>PID</span>
      <span style={{ ...styles.col, minWidth: 60 }}>ERRORS</span>
      <span style={{ ...styles.col, minWidth: 100 }}>LAST ACTIVITY</span>
    </div>
    <div style={styles.pluginList}>
      {plugins.length === 0 && <div style={styles.empty}>No plugins loaded</div>}
      {plugins.map((p) => {
        const c = dotColor(p.state);
        return (
          <div key={p.name} style={styles.row}>
            <span style={{ ...styles.col, minWidth: 180, color: colors.white }}>
              <span style={{ ...styles.dot, background: c }} />{p.name}
            </span>
            <span style={{ ...styles.col, minWidth: 100, color: c }}>{p.state}</span>
            <span style={{ ...styles.col, minWidth: 60, color: colors.dimText }}>{p.pid || "—"}</span>
            <span style={{ ...styles.col, minWidth: 60, color: p.errorCount > 0 ? colors.red : colors.dimText }}>{p.errorCount}</span>
            <span style={{ ...styles.col, minWidth: 100, color: colors.dimText }}>{ago(p.lastActivity)}</span>
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
  feedList: { flex: 1, overflow: "auto", minHeight: 80 },
  pluginList: { flex: 1, overflow: "auto", minHeight: 80 },
  empty: { padding: 16, color: colors.dimText, fontFamily: fonts.mono, fontSize: 12 },
  row: { display: "flex", gap: 8, padding: "4px 12px", borderBottom: `1px solid ${colors.border}`, fontFamily: fonts.mono, fontSize: 11, alignItems: "center" },
  dot: { display: "inline-block", width: 7, height: 7, borderRadius: "50%", marginRight: 6 },
};
