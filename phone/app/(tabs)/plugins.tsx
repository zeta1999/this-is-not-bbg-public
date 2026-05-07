import React, { useState, useEffect, useRef } from "react";
import { View, Text, FlatList, TouchableOpacity, StyleSheet, ScrollView, Image, Modal } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { getServerUrl, getToken } from "../../src/connection";
import { useStore } from "../../src/store";

interface PluginScreen {
  id: string;
  plugin: string;
  label: string;
  icon: string;
  topic: string;
}

interface StyledLine {
  text: string;
  style: string;
}

interface CellAddress { row: number; col: number; }
interface CellStyle { fg?: string; bg?: string; bold?: boolean; }
interface PluginCell {
  address: CellAddress;
  style?: CellStyle;
  type: string;
  text?: string;
  label?: string;
  value?: any;
  precision?: number;
  unit?: string;
  delta?: string;
  col_span?: number;
  component_id?: string;
}

const styleToColor: Record<string, string> = {
  header: colors.amber,
  normal: colors.text,
  green: colors.green,
  red: colors.red,
  dim: colors.muted,
  warn: colors.yellow,
  cyan: "#00BFFF",
  white: colors.text,
};

function cellColor(name?: string): string {
  if (!name) return colors.text;
  return styleToColor[name] || name;
}

// artifactUri turns a plugin-supplied src (NOTBBG:/abs/path,
// /abs/path, or http(s)://…) into a URI react-native Image can
// fetch. Local paths route through the auth-gated server endpoint
// since the phone has no host filesystem access.
function artifactUri(src: string | undefined): string {
  if (!src) return "";
  if (/^https?:\/\//i.test(src)) return src;
  const path = src.startsWith("NOTBBG:") ? src.slice("NOTBBG:".length) : src;
  if (!path.startsWith("/")) return src;
  const url = getServerUrl();
  const token = getToken();
  if (!url || !token) return "";
  return `${url}/api/v1/artifact?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
}

const ImageCellView: React.FC<{ cell: PluginCell }> = ({ cell }) => {
  const [zoom, setZoom] = useState(false);
  const src = (cell as any).src as string | undefined;
  const alt = (cell as any).alt as string | undefined;
  const uri = artifactUri(src);
  if (!uri) {
    return (
      <Text style={[s.gridCell, { color: colors.muted }]} numberOfLines={1}>
        {cell.label ? cell.label + " " : ""}📎 [IMG {alt || "missing"}]
      </Text>
    );
  }
  const w = (cell as any).width as number | undefined;
  const h = (cell as any).height as number | undefined;
  const tw = w && w > 0 ? Math.min(w, 320) : 240;
  const th = h && h > 0 ? Math.round(tw * (h / (w || tw))) : 120;
  return (
    <>
      <TouchableOpacity onPress={() => setZoom(true)}>
        <Image
          source={{ uri }}
          style={{ width: tw, height: th }}
          resizeMode="contain"
          accessibilityLabel={alt}
        />
      </TouchableOpacity>
      <Modal visible={zoom} transparent animationType="fade" onRequestClose={() => setZoom(false)}>
        <TouchableOpacity
          activeOpacity={1}
          onPress={() => setZoom(false)}
          style={{ flex: 1, backgroundColor: "rgba(0,0,0,0.9)", alignItems: "center", justifyContent: "center" }}
        >
          <Image source={{ uri }} style={{ width: "95%", height: "85%" }} resizeMode="contain" />
        </TouchableOpacity>
      </Modal>
    </>
  );
};

function usePlugins() {
  const { pluginScreens, pluginLines, pluginCells } = useStore();
  return {
    screens: pluginScreens,
    lines: pluginLines as Record<string, StyledLine[]>,
    cells: pluginCells as Record<string, PluginCell[]>,
  };
}

// Render a single cell as text.
function renderCellText(cell: PluginCell): { text: string; color: string; bold: boolean } {
  const color = cell.style?.fg ? cellColor(cell.style.fg) : colors.text;
  const bold = cell.style?.bold || false;

  switch (cell.type) {
    case "text":
      return { text: cell.text || "", color, bold };

    case "input_decimal":
    case "input_integer": {
      const val = typeof cell.value === "number" ? cell.value : 0;
      const prec = cell.type === "input_integer" ? 0 : (cell.precision || 2);
      const label = cell.label ? `${cell.label} ` : "";
      return { text: `${label}[${val.toFixed(prec)}]`, color: cellColor("cyan"), bold: false };
    }

    case "input_enum":
      return { text: `${cell.label || ""} [${cell.value}▼]`, color: cellColor("cyan"), bold: false };

    case "input_string":
    case "input_selection":
      return { text: `${cell.label || ""} [${cell.value || ""}]`, color: cellColor("cyan"), bold: false };

    case "input_script": {
      const preview = String(cell.value || "").replace(/\n/g, " ").slice(0, 30);
      return { text: `${cell.label || ""} [${preview}...]`, color: cellColor("cyan"), bold: false };
    }

    case "number": {
      const val = typeof cell.value === "number" ? cell.value : 0;
      const prec = cell.precision || 2;
      const arrow = cell.delta === "up" ? "▲" : cell.delta === "down" ? "▼" : "";
      const numColor = cell.style?.fg ? cellColor(cell.style.fg) : (val > 0 ? colors.green : val < 0 ? colors.red : colors.text);
      const label = cell.label ? `${cell.label} ` : "";
      return { text: `${label}${arrow}${val.toFixed(prec)}${cell.unit || ""}`, color: numColor, bold: false };
    }

    case "formula": {
      const val = typeof cell.value === "number" ? cell.value : 0;
      return { text: `${cell.label || ""} ${val.toFixed(cell.precision || 4)}`, color: colors.green, bold: false };
    }

    case "chart": {
      // Phone is read-only and has limited vertical space — render
      // each series as an 8-row block-character sparkline, same
      // alphabet as the TUI. Multiple series concatenate with a
      // space between names.
      const blocks = "▁▂▃▄▅▆▇█";
      const series = (cell as any).series as { name?: string; values?: number[]; color?: string }[] | undefined;
      if (!series || series.length === 0) {
        return { text: `${cell.label || ""} —`, color: colors.muted, bold: false };
      }
      const parts = series.map((s) => {
        const values = s.values || [];
        if (values.length === 0) return "—";
        let min = values[0], max = values[0];
        for (const v of values) { if (v < min) min = v; if (v > max) max = v; }
        const span = max - min;
        const spark = values.map((v) => {
          const idx = span > 0 ? Math.floor(((v - min) / span) * (blocks.length - 1)) : 0;
          return blocks[idx];
        }).join("");
        return s.name ? `${s.name}:${spark}` : spark;
      });
      return { text: `${cell.label ? cell.label + " " : ""}${parts.join("  ")}`, color: cellColor("cyan"), bold: false };
    }

    case "table": {
      // Flatten the table to "h1 h2 | v1 v2 / v1 v2" so it fits in
      // a single inline string on mobile. Desktop/TUI render as
      // proper tables; phone is space-constrained.
      const cols = (cell as any).columns as { header: string }[] | undefined;
      const rows = (cell as any).rows as string[][] | undefined;
      if (!cols || cols.length === 0) return { text: `${cell.label || ""} (empty)`, color: colors.muted, bold: false };
      const header = cols.map((c) => c.header).join(" ");
      const body = (rows || []).map((r) => (r || []).join(" ")).join(" / ");
      return { text: `${cell.label ? cell.label + " " : ""}${header} | ${body}`, color, bold: false };
    }

    case "image": {
      // Text-only placeholder on phone — the existing renderer is
      // a single-line text row, so full-image rendering lives
      // outside this helper. Phone shows alt + src for visibility.
      const src = (cell as any).src as string | undefined;
      const alt = (cell as any).alt as string | undefined;
      const tag = alt ? `[IMG ${alt}]` : "[IMG]";
      return { text: `${cell.label ? cell.label + " " : ""}📎 ${tag} ${src || ""}`.trim(), color: cellColor("cyan"), bold: false };
    }

    default:
      return { text: cell.text || "", color, bold };
  }
}

async function sendPluginCancel(topic: string, jobID: string = "*") {
  const url = getServerUrl();
  const token = getToken();
  if (!url || !token) return;
  try {
    await fetch(`${url}/api/v1/plugin/cancel?token=${encodeURIComponent(token)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ topic, job_id: jobID }),
    });
  } catch {
    // ignore — the server either saw the request or we're offline
  }
}

function CellGridView({ gridCells, screenTopic }: { gridCells: PluginCell[]; screenTopic?: string }) {
  // Group by row, deduping by (row, col) — last write wins.
  // Some plugins emit transiently overlapping addresses during
  // partial-update batches; rendering both throws React's
  // "two children with the same key" warning, plus the older
  // payload visually flickers under the newer one.
  const cellAt = new Map<string, PluginCell>();
  for (const c of gridCells) {
    cellAt.set(`${c.address.row}-${c.address.col}`, c);
  }
  const rows = new Map<number, PluginCell[]>();
  for (const c of cellAt.values()) {
    const r = c.address.row;
    if (!rows.has(r)) rows.set(r, []);
    rows.get(r)!.push(c);
  }
  const sortedRows = Array.from(rows.keys()).sort((a, b) => a - b);

  // A running progress cell surfaces a Cancel button up top.
  const hasRunningJob = gridCells.some(
    (c) => c.component_id === "progress" && typeof c.value === "number" && c.value > 0 && c.value < 1,
  );

  return (
    <ScrollView style={{ flex: 1 }} contentContainerStyle={s.content}>
      {hasRunningJob && screenTopic && (
        <TouchableOpacity
          onPress={() => sendPluginCancel(screenTopic)}
          style={s.cancelBtn}
        >
          <Text style={s.cancelText}>Cancel running job</Text>
        </TouchableOpacity>
      )}
      {sortedRows.map((rowNum) => {
        const rowCells = rows.get(rowNum)!.sort((a, b) => a.address.col - b.address.col);
        return (
          <View key={rowNum} style={s.gridRow}>
            {rowCells.map((c) => {
              if (c.type === "image") {
                return <ImageCellView key={`${c.address.row}-${c.address.col}`} cell={c} />;
              }
              const { text, color, bold } = renderCellText(c);
              return (
                <Text
                  key={`${c.address.row}-${c.address.col}`}
                  style={[
                    s.gridCell,
                    { color, fontWeight: bold ? "900" : "400" },
                    (c.col_span || 1) > 1 && { flex: c.col_span },
                  ]}
                  numberOfLines={1}
                >
                  {text}
                </Text>
              );
            })}
          </View>
        );
      })}
    </ScrollView>
  );
}

export default function PluginsScreen() {
  const { screens, lines, cells } = usePlugins();
  const [activeScreen, setActiveScreen] = useState(0);
  // Refs to the selector ScrollView + each tab button so chevron
  // taps can scroll the active tab into view (otherwise installs
  // with > 4 plugins look like "only four are listed" on phones —
  // the rest are off-screen, no affordance to find them).
  const scrollRef = useRef<ScrollView>(null);
  const tabLayoutsRef = useRef<Record<number, { x: number; w: number }>>({});
  const scrollWidthRef = useRef<number>(0);

  // When activeScreen changes, scroll the active tab into view
  // (with some padding so the next tab peeks into frame as a hint
  // there's more to the right).
  useEffect(() => {
    const layout = tabLayoutsRef.current[activeScreen];
    if (!layout || !scrollRef.current) return;
    const target = Math.max(0, layout.x - 40);
    scrollRef.current.scrollTo({ x: target, animated: true });
  }, [activeScreen]);

  const stepScreen = (delta: number) => {
    if (!screens.length) return;
    const next = (activeScreen + delta + screens.length) % screens.length;
    setActiveScreen(next);
  };

  if (screens.length === 0) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>PLUGINS</Text>
        <View style={s.empty}>
          <Text style={s.emptyText}>No plugins installed.</Text>
          <Text style={s.emptyText}>Install plugins on the server to see them here.</Text>
        </View>
      </View>
    );
  }

  const current = screens[activeScreen] || screens[0];
  const gridCells = cells[current?.topic];
  const currentLines = lines[current?.topic] || [];

  return (
    <View style={presets.screen}>
      {/* Plugin selector — chevrons + count + horizontally
          scrollable label list. The chevrons advertise that
          there's more than fits in the viewport (the original
          ScrollView-only layout looked like "only four plugins"
          to operators with > 4 installed). */}
      {screens.length > 1 && (
        <View style={s.selectorRow}>
          <TouchableOpacity
            onPress={() => stepScreen(-1)}
            style={s.chevron}
            accessibilityLabel="Previous plugin"
          >
            <Text style={s.chevronText}>‹</Text>
          </TouchableOpacity>
          <ScrollView
            ref={scrollRef}
            horizontal
            showsHorizontalScrollIndicator={false}
            style={s.selector}
            onContentSizeChange={(w) => { scrollWidthRef.current = w; }}
          >
            {screens.map((scr, idx) => (
              <TouchableOpacity
                key={scr.id}
                onPress={() => setActiveScreen(idx)}
                onLayout={(e) => {
                  const { x, width } = e.nativeEvent.layout;
                  tabLayoutsRef.current[idx] = { x, w: width };
                }}
                style={[s.selectorBtn, idx === activeScreen && s.selectorBtnActive]}
              >
                <Text style={[s.selectorText, idx === activeScreen && s.selectorTextActive]}>
                  {scr.label}
                </Text>
              </TouchableOpacity>
            ))}
          </ScrollView>
          <Text style={s.count}>
            {activeScreen + 1}/{screens.length}
          </Text>
          <TouchableOpacity
            onPress={() => stepScreen(1)}
            style={s.chevron}
            accessibilityLabel="Next plugin"
          >
            <Text style={s.chevronText}>›</Text>
          </TouchableOpacity>
        </View>
      )}

      {/* Screen content: cell grid or legacy lines */}
      {gridCells && gridCells.length > 0 ? (
        <CellGridView gridCells={gridCells} screenTopic={current?.topic} />
      ) : (
        <FlatList
          data={currentLines}
          keyExtractor={(_, i) => String(i)}
          renderItem={({ item }) => (
            <Text
              style={[
                s.line,
                { color: styleToColor[item.style] || colors.text },
                item.style === "header" && { fontWeight: "900" },
              ]}
            >
              {item.text}
            </Text>
          )}
          contentContainerStyle={s.content}
        />
      )}
    </View>
  );
}

const s = StyleSheet.create({
  cancelBtn: {
    alignSelf: "flex-end",
    backgroundColor: colors.red,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: 4,
    marginBottom: spacing.sm,
  },
  cancelText: {
    fontFamily: fonts.mono,
    fontSize: 11,
    fontWeight: "700",
    color: "#fff",
    textTransform: "uppercase",
  },
  empty: { padding: spacing.lg },
  emptyText: {
    fontFamily: fonts.mono,
    fontSize: 12,
    color: colors.muted,
    marginBottom: spacing.sm,
  },
  selector: {
    flexGrow: 1,
    flexShrink: 1,
    paddingVertical: spacing.sm,
  },
  selectorRow: {
    flexDirection: "row",
    alignItems: "center",
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
    paddingHorizontal: spacing.sm,
  },
  chevron: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
  },
  chevronText: {
    fontFamily: fonts.mono,
    fontWeight: "900",
    fontSize: 22,
    color: colors.amber,
    lineHeight: 24,
  },
  count: {
    fontFamily: fonts.mono,
    fontSize: 11,
    fontWeight: "700",
    color: colors.muted,
    paddingHorizontal: 6,
    minWidth: 38,
    textAlign: "center",
  },
  selectorBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: 4,
    marginRight: spacing.sm,
    borderWidth: 1,
    borderColor: "transparent",
  },
  selectorBtnActive: {
    borderColor: colors.amber,
    backgroundColor: colors.amberDim,
  },
  selectorText: {
    fontFamily: fonts.mono,
    fontWeight: "700",
    fontSize: 11,
    color: colors.muted,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  selectorTextActive: {
    color: colors.amber,
  },
  content: {
    padding: spacing.md,
  },
  line: {
    fontFamily: fonts.mono,
    fontSize: 13,
    lineHeight: 20,
  },
  gridRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    marginBottom: 2,
  },
  gridCell: {
    fontFamily: fonts.mono,
    fontSize: 12,
    lineHeight: 18,
    flex: 1,
    paddingHorizontal: 4,
  },
});
