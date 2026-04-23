import React, { useState, useRef, useEffect, Suspense } from "react";

const VolSurface3D = React.lazy(() => import("./VolSurface3D").then(m => ({ default: m.VolSurface3D })));
const SwaptionCube3D = React.lazy(() => import("./SwaptionCube3D").then(m => ({ default: m.SwaptionCube3D })));
import { colors, fonts } from "../styles/theme";
import { PluginCell, CellStyle, EnumOption } from "../store";

interface Props {
  label: string;
  cells: PluginCell[];
  screenTopic: string;
  screenId: string;
}

function cellColor(name?: string): string {
  if (!name) return colors.white;
  const map: Record<string, string> = {
    green: colors.green, red: colors.red, cyan: colors.cyan,
    yellow: colors.yellow, dim: colors.dimText, white: colors.white,
    warn: colors.yellow, gray: colors.dimText,
  };
  return map[name] || name;
}

function styleToCSS(s?: CellStyle): React.CSSProperties {
  if (!s) return {};
  return {
    color: s.fg ? cellColor(s.fg) : undefined,
    backgroundColor: s.bg ? cellColor(s.bg) : undefined,
    fontWeight: s.bold ? 700 : undefined,
    fontStyle: s.italic ? "italic" : undefined,
    textDecoration: s.underline ? "underline" : undefined,
  };
}

function formatNumber(val: number, precision: number): string {
  return val.toFixed(precision || 2);
}

function isInputType(type: string): boolean {
  return type.startsWith("input_");
}

function getToken(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get("token") || "";
}

async function sendPluginInput(topic: string, screenId: string, row: number, col: number, value: any) {
  const url = "http://localhost:9474";
  const token = getToken();
  try {
    await fetch(`${url}/api/v1/plugin/input?token=${token}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ topic, screen_id: screenId, address: { row, col }, value }),
    });
  } catch (e) {
    console.error("sendPluginInput failed:", e);
  }
}

// Inline editor for a cell.
const CellEditor: React.FC<{
  cell: PluginCell;
  onSubmit: (value: any) => void;
  onCancel: () => void;
}> = ({ cell, onSubmit, onCancel }) => {
  const [val, setVal] = useState(String(cell.value ?? ""));
  const ref = useRef<HTMLInputElement | HTMLSelectElement>(null);

  useEffect(() => { ref.current?.focus(); }, []);

  if (cell.type === "input_enum" && cell.options) {
    return (
      <select
        ref={ref as any}
        value={String(cell.value)}
        onChange={(e) => onSubmit(e.target.value)}
        onBlur={onCancel}
        style={editorStyle}
      >
        {cell.options.map((o: EnumOption) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    );
  }

  // Script cells get a textarea.
  if (cell.type === "input_script") {
    return (
      <textarea
        ref={ref as any}
        value={val}
        onChange={(e) => setVal(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") { e.preventDefault(); onCancel(); }
          if (e.key === "s" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); onSubmit(val); }
        }}
        style={{ ...editorStyle, minHeight: 200, resize: "vertical", whiteSpace: "pre", fontFamily: fonts.mono }}
      />
    );
  }

  const inputType = cell.type === "input_decimal" || cell.type === "input_integer" ? "number" : "text";
  return (
    <input
      ref={ref as any}
      type={inputType}
      value={val}
      onChange={(e) => setVal(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          const parsed = cell.type === "input_decimal" ? parseFloat(val) :
                         cell.type === "input_integer" ? parseInt(val, 10) : val;
          onSubmit(parsed);
        }
        if (e.key === "Escape") onCancel();
      }}
      onBlur={onCancel}
      style={editorStyle}
      step={cell.type === "input_integer" ? 1 : "any"}
    />
  );
};

const editorStyle: React.CSSProperties = {
  background: "#1a1a4a",
  color: colors.white,
  border: `1px solid ${colors.cyan}`,
  fontFamily: fonts.mono,
  fontSize: 12,
  padding: "2px 6px",
  outline: "none",
  width: "100%",
  boxSizing: "border-box",
};

const CellRenderer: React.FC<{
  cell: PluginCell;
  editing: boolean;
  onStartEdit: () => void;
  onSubmit: (value: any) => void;
  onCancel: () => void;
}> = ({ cell, editing, onStartEdit, onSubmit, onCancel }) => {
  const base: React.CSSProperties = {
    ...styleToCSS(cell.style),
    fontFamily: fonts.mono,
    fontSize: 12,
    padding: "3px 8px",
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
    cursor: isInputType(cell.type) ? "pointer" : "default",
  };

  if (editing) {
    return <CellEditor cell={cell} onSubmit={onSubmit} onCancel={onCancel} />;
  }

  const onClick = isInputType(cell.type) ? onStartEdit : undefined;

  switch (cell.type) {
    case "text":
      return <span style={base}>{cell.text}</span>;

    case "input_decimal":
    case "input_integer": {
      const val = typeof cell.value === "number" ? cell.value : 0;
      const prec = cell.type === "input_integer" ? 0 : (cell.precision || 2);
      return (
        <span style={base} onClick={onClick}>
          {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
          <span style={{ color: colors.cyan }}>[{formatNumber(val, prec)}]</span>
        </span>
      );
    }

    case "input_string":
      return (
        <span style={base} onClick={onClick}>
          {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
          <span style={{ color: colors.cyan }}>[{cell.value || ""}]</span>
        </span>
      );

    case "input_enum":
      return (
        <span style={base} onClick={onClick}>
          {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
          <span style={{ color: colors.cyan }}>[{String(cell.value)}&#x25BC;]</span>
        </span>
      );

    case "input_selection":
      return (
        <span style={base} onClick={onClick}>
          {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
          <span style={{ color: colors.cyan }}>[{String(cell.value)}]</span>
        </span>
      );

    case "input_script": {
      const lines = String(cell.value || "").split("\n");
      const previewLines = lines.slice(0, 4);
      if (lines.length > 4) previewLines.push(`... (${lines.length} lines)`);
      return (
        <div style={{ ...base, cursor: "pointer", whiteSpace: "pre-wrap" }} onClick={onClick}>
          {cell.label && <div style={{ color: colors.dimText, marginBottom: 2 }}>{cell.label} <span style={{ color: colors.cyan }}>&#x270E; click to edit</span></div>}
          <div style={{ color: colors.cyan, fontSize: 11, lineHeight: 1.4, fontFamily: fonts.mono }}>
            {previewLines.join("\n")}
          </div>
        </div>
      );
    }

    case "number": {
      const val = typeof cell.value === "number" ? cell.value : 0;
      const prec = cell.precision || 2;
      let numColor = base.color || colors.white;
      if (!cell.style) {
        numColor = val > 0 ? colors.green : val < 0 ? colors.red : colors.white;
      }
      const arrow = cell.delta === "up" ? "▲" : cell.delta === "down" ? "▼" : "";
      return (
        <span style={{ ...base, color: numColor }}>
          {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
          {arrow}{formatNumber(val, prec)}{cell.unit || ""}
        </span>
      );
    }

    case "formula": {
      const val = typeof cell.value === "number" ? cell.value : 0;
      const prec = cell.precision || 4;
      return (
        <span style={{ ...base, color: colors.green }}>
          {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
          {formatNumber(val, prec)}
        </span>
      );
    }

    case "component":
      if (cell.component_id === "vol_surface") {
        try {
          const surfData = typeof cell.value === "string" ? JSON.parse(cell.value) : cell.value;
          return (
            <div style={{ ...base, width: "100%", height: 400 }}>
              <Suspense fallback={<span style={{ color: colors.dimText }}>Loading 3D...</span>}>
                <VolSurface3D data={surfData} />
              </Suspense>
            </div>
          );
        } catch {
          return <span style={{ ...base, color: colors.dimText }}>[vol_surface: invalid data]</span>;
        }
      }
      if (cell.component_id === "swaption_cube") {
        try {
          const cubeData = typeof cell.value === "string" ? JSON.parse(cell.value) : cell.value;
          return (
            <div style={{ ...base, width: "100%", height: 400 }}>
              <Suspense fallback={<span style={{ color: colors.dimText }}>Loading 3D...</span>}>
                <SwaptionCube3D data={cubeData} />
              </Suspense>
            </div>
          );
        } catch {
          return <span style={{ ...base, color: colors.dimText }}>[swaption_cube: invalid data]</span>;
        }
      }
      if (cell.component_id === "progress") {
        const progress = typeof cell.value === "number" ? Math.max(0, Math.min(1, cell.value)) : 0;
        const pct = Math.round(progress * 100);
        const barWidth = 120;
        return (
          <span style={base}>
            {cell.label && <span style={{ color: colors.dimText }}>{cell.label} </span>}
            <span style={{ display: "inline-block", width: barWidth, height: 12, background: "#222", borderRadius: 2, verticalAlign: "middle", marginRight: 6 }}>
              <span style={{ display: "block", width: `${pct}%`, height: "100%", background: colors.cyan, borderRadius: 2 }} />
            </span>
            <span style={{ color: colors.cyan }}>{pct}%</span>
            {cell.text && <span style={{ color: colors.dimText }}> {cell.text}</span>}
          </span>
        );
      }
      return <span style={{ ...base, color: colors.dimText }}>[{cell.component_id}]</span>;

    default:
      return <span style={base}>{cell.text || ""}</span>;
  }
};

export const CellGridPanel: React.FC<Props> = ({ label, cells, screenTopic, screenId }) => {
  const [editingKey, setEditingKey] = useState<string | null>(null);

  if (!cells || cells.length === 0) {
    return (
      <div style={s.container}>
        <div style={s.header}>
          <span style={s.label}>{label}</span>
        </div>
        <div style={s.body}>
          <span style={{ color: colors.dimText }}>Waiting for cell grid data...</span>
        </div>
      </div>
    );
  }

  const rows = new Map<number, PluginCell[]>();
  let maxCol = 0;
  for (const c of cells) {
    const r = c.address.row;
    if (!rows.has(r)) rows.set(r, []);
    rows.get(r)!.push(c);
    const cEnd = c.address.col + (c.col_span || 1) - 1;
    if (cEnd > maxCol) maxCol = cEnd;
  }
  const sortedRows = Array.from(rows.keys()).sort((a, b) => a - b);

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.label}>{label}</span>
        <span style={s.cellCount}>{cells.length} cells</span>
      </div>
      <div style={s.body}>
        <table style={s.table}>
          <tbody>
            {sortedRows.map((rowNum) => {
              const rowCells = rows.get(rowNum)!.sort((a, b) => a.address.col - b.address.col);
              const tds: React.ReactNode[] = [];
              let col = 0;
              for (const c of rowCells) {
                while (col < c.address.col) {
                  tds.push(<td key={`${rowNum}-${col}`} style={s.td} />);
                  col++;
                }
                const span = c.col_span || 1;
                const key = `${c.address.row},${c.address.col}`;
                tds.push(
                  <td key={key} style={s.td} colSpan={span > 1 ? span : undefined}>
                    <CellRenderer
                      cell={c}
                      editing={editingKey === key}
                      onStartEdit={() => setEditingKey(key)}
                      onSubmit={(value) => {
                        setEditingKey(null);
                        sendPluginInput(screenTopic, screenId, c.address.row, c.address.col, value);
                      }}
                      onCancel={() => setEditingKey(null)}
                    />
                  </td>
                );
                col += span;
              }
              return <tr key={rowNum}>{tds}</tr>;
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  container: { display: "flex", flexDirection: "column", height: "100%" },
  header: {
    display: "flex", alignItems: "center", gap: 12,
    padding: "6px 12px", background: "#0D0D0D",
    borderBottom: `1px solid ${colors.border}`, flexShrink: 0,
  },
  label: { fontSize: 13, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  cellCount: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono },
  body: { flex: 1, overflow: "auto", padding: "4px 8px" },
  table: { borderCollapse: "collapse" as const, width: "100%" },
  td: { padding: "2px 4px", verticalAlign: "top", borderBottom: `1px solid #111` },
};
