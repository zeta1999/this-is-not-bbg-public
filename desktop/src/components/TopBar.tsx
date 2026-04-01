import React, { useState, useEffect } from "react";
import { colors, fonts } from "../styles/theme";
import type { TabId } from "../types";

const TABS: TabId[] = ["OHLC", "LOB", "NEWS", "ALERTS", "MON", "LOG", "AGENT"];

interface TopBarProps {
  activeTab: TabId;
  onTabChange: (tab: TabId) => void;
  connected: boolean;
  latencyMs: number;
}

export const TopBar: React.FC<TopBarProps> = ({
  activeTab,
  onTabChange,
  connected,
  latencyMs,
}) => {
  const [clock, setClock] = useState(formatClock());

  useEffect(() => {
    const id = setInterval(() => setClock(formatClock()), 1000);
    return () => clearInterval(id);
  }, []);

  return (
    <div style={styles.bar}>
      {/* Brand */}
      <div style={styles.brand}>
        <span style={styles.brandText}>NOTBBG</span>
        <span style={styles.brandSub}>TERMINAL</span>
      </div>

      {/* Tabs */}
      <div style={styles.tabs}>
        {TABS.map((tab) => (
          <button
            key={tab}
            onClick={() => onTabChange(tab)}
            style={{
              ...styles.tab,
              ...(activeTab === tab ? styles.tabActive : {}),
            }}
          >
            {tab}
          </button>
        ))}
      </div>

      {/* Right side: clock + connection */}
      <div style={styles.right}>
        <span style={styles.clock}>{clock}</span>
        <div style={styles.connectionGroup}>
          <div
            style={{
              ...styles.dot,
              background: connected ? colors.green : colors.red,
              boxShadow: connected
                ? `0 0 6px ${colors.green}`
                : `0 0 6px ${colors.red}`,
            }}
          />
          <span style={{ ...styles.connText, color: connected ? colors.green : colors.red }}>
            {connected ? `${latencyMs}ms` : "OFF"}
          </span>
        </div>
      </div>
    </div>
  );
};

function formatClock(): string {
  const now = new Date();
  const hh = String(now.getUTCHours()).padStart(2, "0");
  const mm = String(now.getUTCMinutes()).padStart(2, "0");
  const ss = String(now.getUTCSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss} UTC`;
}

const styles: Record<string, React.CSSProperties> = {
  bar: {
    display: "flex",
    alignItems: "center",
    height: 40,
    padding: "0 12px",
    background: colors.surface,
    borderBottom: `1px solid ${colors.border}`,
    flexShrink: 0,
    gap: 16,
  },
  brand: {
    display: "flex",
    alignItems: "baseline",
    gap: 6,
  },
  brandText: {
    fontFamily: fonts.mono,
    fontSize: 16,
    fontWeight: 900,
    color: colors.amber,
    letterSpacing: "0.12em",
  },
  brandSub: {
    fontFamily: fonts.mono,
    fontSize: 9,
    color: colors.dimText,
    letterSpacing: "0.15em",
  },
  tabs: {
    display: "flex",
    gap: 2,
    marginLeft: 16,
  },
  tab: {
    fontFamily: fonts.mono,
    fontSize: 11,
    fontWeight: 700,
    letterSpacing: "0.08em",
    padding: "6px 14px",
    background: "transparent",
    border: `1px solid transparent`,
    color: colors.dimText,
    cursor: "pointer",
    borderRadius: 2,
    transition: "all 0.1s",
  },
  tabActive: {
    color: colors.amber,
    background: "#1A1200",
    borderColor: colors.amber,
  },
  right: {
    marginLeft: "auto",
    display: "flex",
    alignItems: "center",
    gap: 16,
  },
  clock: {
    fontFamily: fonts.mono,
    fontSize: 12,
    color: colors.amberDim,
    letterSpacing: "0.06em",
  },
  connectionGroup: {
    display: "flex",
    alignItems: "center",
    gap: 6,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: "50%",
  },
  connText: {
    fontFamily: fonts.mono,
    fontSize: 10,
    fontWeight: 700,
  },
};
