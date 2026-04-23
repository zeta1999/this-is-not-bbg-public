import React, { useState, useEffect, useCallback } from "react";
import { colors, fonts } from "./styles/theme";
import { OHLCChart } from "./components/OHLCChart";
import { LOBViewer } from "./components/LOBViewer";
import { NewsPanel } from "./components/NewsPanel";
import { AlertsPanel } from "./components/AlertsPanel";
import { MonitorPanel } from "./components/MonitorPanel";
import { LogPanel } from "./components/LogPanel";
import { AgentPanel } from "./components/AgentPanel";
import { PluginPanel } from "./components/PluginPanel";
import { CellGridPanel } from "./components/CellGridPanel";
import { TradesPanel } from "./components/TradesPanel";
import { PairModal } from "./components/PairModal";
import { useStore, PluginScreen } from "./store";

const CORE_TABS = ["OHLC", "LOB", "TRADES", "NEWS", "ALERTS", "MON", "LOG", "AGENT"] as const;
type CoreTab = typeof CORE_TABS[number];

const App: React.FC = () => {
  const [activeTab, setActiveTab] = useState<string>("OHLC");
  const [clock, setClock] = useState("");
  const [showPair, setShowPair] = useState(false);
  const store = useStore();

  // Build dynamic tab list: core tabs + plugin screens.
  const allTabs: string[] = [...CORE_TABS, ...store.pluginScreens.map((s) => s.id)];

  // Clock — update every second.
  useEffect(() => {
    const tick = () => {
      const now = new Date();
      setClock(now.toISOString().slice(11, 19) + " UTC");
    };
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, []);

  // Keyboard shortcuts — same as TUI handleKey().
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Don't intercept when typing in an input.
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;

      // 1-9: jump to panel.
      if (e.key >= "1" && e.key <= "9") {
        const idx = parseInt(e.key) - 1;
        if (idx < allTabs.length) setActiveTab(allTabs[idx]);
        return;
      }

      // Tab: next panel.
      if (e.key === "Tab") {
        e.preventDefault();
        setActiveTab((prev) => {
          const idx = allTabs.indexOf(prev);
          return allTabs[(idx + (e.shiftKey ? -1 : 1) + allTabs.length) % allTabs.length];
        });
        return;
      }

      // [/] — instrument navigation.
      if (e.key === "[" || e.key === "ArrowLeft") {
        if (activeTab === "OHLC" && store.ohlcKeys.length > 0) {
          store.setOhlcActiveIdx((store.ohlcActiveIdx - 1 + store.ohlcKeys.length) % store.ohlcKeys.length);
        } else if (activeTab === "LOB" && store.lobKeys.length > 0) {
          store.setLobActiveIdx((store.lobActiveIdx - 1 + store.lobKeys.length) % store.lobKeys.length);
        }
        return;
      }
      if (e.key === "]" || e.key === "ArrowRight") {
        if (activeTab === "OHLC" && store.ohlcKeys.length > 0) {
          store.setOhlcActiveIdx((store.ohlcActiveIdx + 1) % store.ohlcKeys.length);
        } else if (activeTab === "LOB" && store.lobKeys.length > 0) {
          store.setLobActiveIdx((store.lobActiveIdx + 1) % store.lobKeys.length);
        }
        return;
      }

      // -/+ — timeframe cycling (OHLC only).
      if (e.key === "-" && activeTab === "OHLC") { store.cycleTF(-1); return; }
      if ((e.key === "=" || e.key === "+") && activeTab === "OHLC") { store.cycleTF(1); return; }
    };

    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [activeTab, store, allTabs]);

  const renderPanel = () => {
    switch (activeTab) {
      case "OHLC": return <OHLCChart ohlcData={store.ohlcData} ohlcKeys={store.ohlcKeys} activeIdx={store.ohlcActiveIdx} setActiveIdx={store.setOhlcActiveIdx} cycleTF={store.cycleTF} fetchOHLCHistory={store.fetchOHLCHistory} fetchOHLCHistoryStreaming={store.fetchOHLCHistoryStreaming} ohlcLoading={store.ohlcLoading} />;
      case "LOB": return <LOBViewer lobData={store.lobData} lobKeys={store.lobKeys} activeIdx={store.lobActiveIdx} setActiveIdx={store.setLobActiveIdx} />;
      case "TRADES": return <TradesPanel aggs={store.tradeAggs} snaps={store.tradeSnaps} keys={store.tradeKeys} />;
      case "NEWS": return <NewsPanel items={store.newsItems} />;
      case "ALERTS": return <AlertsPanel items={store.alertItems} />;
      case "MON": return <MonitorPanel feeds={store.feedStatuses} />;
      case "LOG": return <LogPanel lines={store.logLines} />;
      case "AGENT": return <AgentPanel />;
      default: {
        // Check if this is a plugin screen.
        const ps = store.pluginScreens.find((s) => s.id === activeTab);
        if (ps) {
          // Prefer cell grid if available, fallback to legacy styled lines.
          const cells = store.pluginCells[ps.topic];
          if (cells && cells.length > 0) {
            return <CellGridPanel label={ps.label} cells={cells} screenTopic={ps.topic} screenId={ps.id} />;
          }
          return <PluginPanel label={ps.label} lines={store.pluginLines[ps.topic] || []} />;
        }
        return null;
      }
    }
  };

  // Bottom bar hints — same as TUI.
  let panelHint = "";
  if (activeTab === "OHLC") panelHint = "  [/]:pair  -/+:timeframe";
  else if (activeTab === "LOB") panelHint = "  [/]:pair";
  else if (activeTab === "NEWS") panelHint = "  click:read  search:filter";

  return (
    <div style={s.root}>
      {/* Top bar */}
      <div style={s.topBar}>
        <span style={s.brand}>NOTBBG</span>
        <span style={s.brandSub}>TERMINAL</span>
        {allTabs.map((tab) => (
          <button key={tab} onClick={() => setActiveTab(tab)}
            style={{ ...s.tab, ...(tab === activeTab ? s.tabActive : {}) }}>
            {tab}
          </button>
        ))}
        <span style={s.msgs}>{store.msgCount >= 50000 ? "50000+ msgs" : `${store.msgCount} msgs`}</span>
        <span style={s.clock}>{clock}</span>
        <button style={s.pairBtn} onClick={() => setShowPair(true)} title="Phone pairing">📱</button>
        <span style={{ ...s.connDot, background: store.connected ? colors.green : colors.red }} />
        <span style={{ color: store.connected ? colors.green : colors.red, ...s.connText }}>
          {store.connected ? "0ms" : "OFF"}
        </span>
      </div>

      {/* Phone pairing modal */}
      <PairModal visible={showPair} onClose={() => setShowPair(false)} />

      {/* Main panel */}
      <div style={s.main}>{renderPanel()}</div>

      {/* Bottom bar */}
      <div style={s.bottomBar}>
        <span>TAB:switch  1-{allTabs.length}:panels{panelHint}  |  {store.connected ? "CONNECTED" : "DISCONNECTED"} | localhost:9474 (SSE)</span>
        <span>{store.ohlcKeys.length} instruments | {store.newsItems.length} news | notbbg v0.2.0</span>
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  root: { display: "flex", flexDirection: "column", height: "100vh", background: colors.bg, fontFamily: fonts.mono, color: colors.white, overflow: "hidden" },
  topBar: { display: "flex", alignItems: "center", gap: 8, padding: "0 12px", height: 36, background: "#0D0D0D", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 },
  brand: { fontSize: 16, fontWeight: 900, color: colors.amber, letterSpacing: "0.1em" },
  brandSub: { fontSize: 9, color: colors.dimText, letterSpacing: "0.15em", marginRight: 12 },
  tab: { fontFamily: fonts.mono, fontSize: 11, fontWeight: 700, padding: "5px 12px", background: "none", border: `1px solid transparent`, color: colors.dimText, cursor: "pointer", borderRadius: 2 },
  tabActive: { color: colors.amber, borderColor: colors.amber, background: "#1A1200" },
  msgs: { marginLeft: "auto", fontSize: 10, color: colors.dimText, letterSpacing: "0.04em" },
  clock: { fontSize: 12, color: colors.amber, letterSpacing: "0.05em" },
  connDot: { width: 8, height: 8, borderRadius: "50%", marginLeft: 8 },
  pairBtn: { background: "none", border: "none", fontSize: 14, cursor: "pointer", padding: "2px 6px", marginLeft: 8 },
  connText: { fontSize: 10, fontWeight: 700, marginLeft: 4 },
  main: { flex: 1, overflow: "hidden" },
  bottomBar: { display: "flex", justifyContent: "space-between", height: 22, padding: "0 12px", alignItems: "center", background: "#0D0D0D", borderTop: `1px solid ${colors.border}`, fontSize: 10, color: colors.dimText, flexShrink: 0 },
};

export default App;
