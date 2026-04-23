import React, { useState, useEffect } from "react";
import { View, Text, FlatList, TouchableOpacity, StyleSheet, ScrollView } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { getServerUrl, getToken, onConnectionChange } from "../../src/connection";

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

function usePlugins() {
  const [screens, setScreens] = useState<PluginScreen[]>([]);
  const [lines, setLines] = useState<Record<string, StyledLine[]>>({});
  const [cells, setCells] = useState<Record<string, PluginCell[]>>({});
  const [token, setToken] = useState(getToken());

  useEffect(() => {
    const unsub = onConnectionChange(() => setToken(getToken()));
    return unsub;
  }, []);

  useEffect(() => {
    if (!token) return;
    let active = true;

    async function poll() {
      const url = getServerUrl();
      while (active) {
        try {
          // Fetch registry.
          const regResp = await fetch(
            `${url}/api/v1/snapshot?topic=plugin.registry&mode=latest&token=${token}`
          );
          const regData = await regResp.json();
          if (Array.isArray(regData) && regData.length > 0) {
            const payload = regData[0];
            if (payload.screens) setScreens(payload.screens);
          }

          // Fetch screen content for each known plugin.
          for (const s of screens) {
            const resp = await fetch(
              `${url}/api/v1/snapshot?topic=${s.topic}&mode=latest&token=${token}`
            );
            const data = await resp.json();
            if (Array.isArray(data) && data.length > 0) {
              const payload = data[0];
              if (payload.version === "cellgrid/v1" && Array.isArray(payload.cells)) {
                setCells((prev) => ({ ...prev, [s.topic]: payload.cells }));
              } else if (payload.lines) {
                setLines((prev) => ({ ...prev, [s.topic]: payload.lines }));
              }
            }
          }
        } catch {}
        await new Promise((r) => setTimeout(r, 3000));
      }
    }

    poll();
    return () => { active = false; };
  }, [token, screens.length]);

  return { screens, lines, cells };
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

    default:
      return { text: cell.text || "", color, bold };
  }
}

function CellGridView({ gridCells }: { gridCells: PluginCell[] }) {
  // Group by row.
  const rows = new Map<number, PluginCell[]>();
  for (const c of gridCells) {
    const r = c.address.row;
    if (!rows.has(r)) rows.set(r, []);
    rows.get(r)!.push(c);
  }
  const sortedRows = Array.from(rows.keys()).sort((a, b) => a - b);

  return (
    <ScrollView style={{ flex: 1 }} contentContainerStyle={s.content}>
      {sortedRows.map((rowNum) => {
        const rowCells = rows.get(rowNum)!.sort((a, b) => a.address.col - b.address.col);
        return (
          <View key={rowNum} style={s.gridRow}>
            {rowCells.map((c) => {
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
      {/* Plugin selector */}
      {screens.length > 1 && (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={s.selector}>
          {screens.map((scr, idx) => (
            <TouchableOpacity
              key={scr.id}
              onPress={() => setActiveScreen(idx)}
              style={[s.selectorBtn, idx === activeScreen && s.selectorBtnActive]}
            >
              <Text style={[s.selectorText, idx === activeScreen && s.selectorTextActive]}>
                {scr.label}
              </Text>
            </TouchableOpacity>
          ))}
        </ScrollView>
      )}

      {/* Screen content: cell grid or legacy lines */}
      {gridCells && gridCells.length > 0 ? (
        <CellGridView gridCells={gridCells} />
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
  empty: { padding: spacing.lg },
  emptyText: {
    fontFamily: fonts.mono,
    fontSize: 12,
    color: colors.muted,
    marginBottom: spacing.sm,
  },
  selector: {
    flexGrow: 0,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
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
