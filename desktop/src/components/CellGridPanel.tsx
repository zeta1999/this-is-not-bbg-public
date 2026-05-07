import React, { useState, useRef, useEffect, useMemo, Suspense } from "react";

const VolSurface3D = React.lazy(() => import("./VolSurface3D").then(m => ({ default: m.VolSurface3D })));
const SwaptionCube3D = React.lazy(() => import("./SwaptionCube3D").then(m => ({ default: m.SwaptionCube3D })));
import { colors, fonts } from "../styles/theme";
import { PluginCell, CellStyle, EnumOption, TableColumn } from "../store";

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

async function sendPluginCancel(topic: string, jobID: string = "*") {
  const url = "http://localhost:9474";
  const token = getToken();
  try {
    await fetch(`${url}/api/v1/plugin/cancel?token=${token}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ topic, job_id: jobID }),
    });
  } catch (e) {
    console.error("sendPluginCancel failed:", e);
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

  // Script cells get a light-touch code-editor upgrade: line
  // numbers in a gutter, Tab/Shift+Tab indents, Enter preserves the
  // previous line's leading whitespace. Full syntax highlighting via
  // Monaco is deferred — this covers the workflow without the ~3 MB
  // bundle cost.
  if (cell.type === "input_script") {
    return <ScriptEditor val={val} setVal={setVal} onSubmit={onSubmit} onCancel={onCancel} />;
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

    case "chart":
      return <ChartCellView cell={cell} />;

    case "table":
      return <TableCellView cell={cell} />;

    case "image":
      return <ImageCellView cell={cell} />;

    default:
      return <span style={base}>{cell.text || ""}</span>;
  }
};

import { tokenizeAria, ARIA_COLORS } from "../aria-syntax";

// ScriptEditor is a textarea with a line-number gutter + Tab and
// auto-indent behavior. A transparent textarea sits over a
// styled <pre> so caret + selection still feel native while the
// rendered text gets coloured per Aria-DSL token. Shares no
// state with Monaco / CodeMirror so the desktop bundle stays lean.
const ScriptEditor: React.FC<{
  val: string;
  setVal: (s: string) => void;
  onSubmit: (v: any) => void;
  onCancel: () => void;
}> = ({ val, setVal, onSubmit, onCancel }) => {
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  const gutterRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    taRef.current?.focus();
  }, []);

  const lines = (val || "").split("\n");
  const gutter = lines.map((_, i) => i + 1).join("\n");

  // Keep the gutter scroll in sync with the textarea so line numbers
  // stay aligned when the textarea is tall enough to scroll.
  const onScroll = () => {
    if (gutterRef.current && taRef.current) {
      gutterRef.current.scrollTop = taRef.current.scrollTop;
    }
  };

  const leadingWS = (s: string) => {
    let i = 0;
    while (i < s.length && (s[i] === " " || s[i] === "\t")) i++;
    return s.slice(0, i);
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    const start = ta.selectionStart;
    const end = ta.selectionEnd;

    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
      return;
    }
    if (e.key === "s" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      onSubmit(val);
      return;
    }
    if (e.key === "Tab") {
      e.preventDefault();
      if (start === end) {
        // Single-caret: insert two spaces.
        const next = val.slice(0, start) + "  " + val.slice(end);
        setVal(next);
        requestAnimationFrame(() => {
          ta.selectionStart = ta.selectionEnd = start + 2;
        });
      } else {
        // Range: indent each selected line. Shift+Tab outdents.
        const lineStart = val.lastIndexOf("\n", start - 1) + 1;
        const head = val.slice(0, lineStart);
        const block = val.slice(lineStart, end);
        const tail = val.slice(end);
        const transformed = block
          .split("\n")
          .map((ln) => (e.shiftKey ? ln.replace(/^ {1,2}/, "") : "  " + ln))
          .join("\n");
        setVal(head + transformed + tail);
      }
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const lineStart = val.lastIndexOf("\n", start - 1) + 1;
      const indent = leadingWS(val.slice(lineStart, start));
      const next = val.slice(0, start) + "\n" + indent + val.slice(end);
      setVal(next);
      requestAnimationFrame(() => {
        ta.selectionStart = ta.selectionEnd = start + 1 + indent.length;
      });
      return;
    }
  };

  return (
    <div style={{ display: "flex", alignItems: "stretch", background: colors.bg, border: `1px solid ${colors.border}`, borderRadius: 3 }}>
      <div
        ref={gutterRef}
        style={{
          color: colors.dimText,
          fontFamily: fonts.mono,
          fontSize: 12,
          textAlign: "right",
          padding: "6px 6px 6px 8px",
          minWidth: 32,
          whiteSpace: "pre",
          overflow: "hidden",
          userSelect: "none",
          background: "#0A0A0A",
          borderRight: `1px solid ${colors.border}`,
          lineHeight: "18px",
        }}
      >
        {gutter}
      </div>
      <div style={{ position: "relative", flex: 1, minHeight: 200 }}>
        {/* Coloured render layer: a <pre> showing tokenised text
            sits underneath the transparent textarea. Both share
            font / size / padding / line-height so caret + visible
            character cells line up to the pixel. */}
        <pre
          aria-hidden="true"
          style={{
            position: "absolute",
            inset: 0,
            margin: 0,
            padding: "6px 8px",
            fontFamily: fonts.mono,
            fontSize: 12,
            lineHeight: "18px",
            whiteSpace: "pre",
            tabSize: 2,
            pointerEvents: "none",
            overflow: "hidden",
            background: colors.bg,
            color: colors.white,
          }}
        >
          {tokenizeAria(val).map((t, i) => (
            <span key={i} style={{ color: ARIA_COLORS[t.kind] }}>{t.text}</span>
          ))}
          {/* Trailing newline marker so the last empty line still
              has height in the highlight layer. */}
          {"\n"}
        </pre>
        <textarea
          ref={taRef}
          value={val}
          onChange={(e) => setVal(e.target.value)}
          onKeyDown={onKeyDown}
          onScroll={onScroll}
          spellCheck={false}
          style={{
            position: "relative",
            width: "100%",
            height: "100%",
            minHeight: 200,
            padding: "6px 8px",
            background: "transparent",
            color: "transparent",
            caretColor: colors.amber,
            border: "none",
            outline: "none",
            resize: "vertical",
            whiteSpace: "pre",
            fontFamily: fonts.mono,
            fontSize: 12,
            lineHeight: "18px",
            tabSize: 2,
          }}
        />
      </div>
    </div>
  );
};

// artifactUrl turns a plugin-supplied src (NOTBBG:/abs/path,
// /abs/path, or http(s)://…) into a URL the browser can fetch.
// Local paths route through the auth-gated /api/v1/artifact
// endpoint so the same code works in Electron *and* a regular
// browser, and so the server can enforce the path allowlist.
function artifactUrl(src: string | undefined): string {
  if (!src) return "";
  if (/^https?:\/\//i.test(src)) return src;
  const path = src.startsWith("NOTBBG:") ? src.slice("NOTBBG:".length) : src;
  if (!path.startsWith("/")) return src;
  const params = new URLSearchParams(window.location.search);
  const token = params.get("token") || "";
  return `http://localhost:9474/api/v1/artifact?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
}

// ImageCellView routes plugin image cells through the auth-gated
// server endpoint, falls back to a labelled placeholder when the
// fetch fails, and supports click-to-zoom.
const ImageCellView: React.FC<{ cell: PluginCell }> = ({ cell }) => {
  const [broken, setBroken] = useState(false);
  const [zoom, setZoom] = useState(false);
  const src = artifactUrl(cell.src);

  useEffect(() => {
    if (!zoom) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setZoom(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [zoom]);

  if (!src || broken) {
    return (
      <span style={{ color: colors.dimText }}>
        {cell.label ? `${cell.label} ` : ""}📎 [IMG {cell.alt || "missing"}]
        {cell.src ? ` ${cell.src}` : ""}
      </span>
    );
  }
  return (
    <>
      <img
        src={src}
        alt={cell.alt || ""}
        width={cell.width || undefined}
        height={cell.height || undefined}
        onError={() => setBroken(true)}
        onClick={() => setZoom(true)}
        title="Click to zoom"
        style={{ display: "inline-block", maxWidth: "100%", verticalAlign: "middle", cursor: "zoom-in" }}
      />
      {zoom && (
        <div
          onClick={() => setZoom(false)}
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(0,0,0,0.85)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            zIndex: 1000,
            cursor: "zoom-out",
          }}
        >
          <img
            src={src}
            alt={cell.alt || ""}
            style={{ maxWidth: "95vw", maxHeight: "95vh", objectFit: "contain" }}
          />
        </div>
      )}
    </>
  );
};

// ChartCellView draws one SVG polyline per series inside a compact
// box. Kept simple on purpose — plugins that need rich interactivity
// fall back to a `component` cell with a dedicated ID.
const ChartCellView: React.FC<{ cell: PluginCell }> = ({ cell }) => {
  const series = cell.series || [];
  if (series.length === 0) {
    return <span style={{ color: colors.dimText }}>{cell.label ? `${cell.label} ` : ""}—</span>;
  }
  const width = 180;
  const height = 36;
  const pad = 2;

  // Common min/max across all series so they share the y-axis.
  let min = Infinity;
  let max = -Infinity;
  for (const s of series) {
    for (const v of s.values || []) {
      if (v < min) min = v;
      if (v > max) max = v;
    }
  }
  if (!isFinite(min) || !isFinite(max)) {
    return <span style={{ color: colors.dimText }}>{cell.label ? `${cell.label} ` : ""}—</span>;
  }
  if (min === max) {
    min -= 1;
    max += 1;
  }

  const colorFor = (name?: string, fallback: string = colors.cyan): string => {
    switch ((name || "").toLowerCase()) {
      case "green": return colors.green;
      case "red": return colors.red;
      case "amber": return colors.amber;
      case "cyan": return colors.cyan;
      case "white": return colors.white;
      default: return name && name.startsWith("#") ? name : fallback;
    }
  };

  const points = (values: number[]): string => {
    if (values.length === 0) return "";
    const stepX = values.length === 1 ? 0 : (width - 2 * pad) / (values.length - 1);
    return values
      .map((v, i) => {
        const x = pad + stepX * i;
        const y = height - pad - ((v - min) / (max - min)) * (height - 2 * pad);
        return `${x.toFixed(2)},${y.toFixed(2)}`;
      })
      .join(" ");
  };

  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      {cell.label && <span style={{ color: colors.dimText }}>{cell.label}</span>}
      <svg width={width} height={height} style={{ verticalAlign: "middle" }}>
        {series.map((s, i) => {
          const stroke = colorFor(s.color, [colors.cyan, colors.amber, colors.green, colors.red][i % 4]);
          return (
            <polyline
              key={i}
              points={points(s.values || [])}
              fill="none"
              stroke={stroke}
              strokeWidth={1.5}
            />
          );
        })}
      </svg>
    </span>
  );
};

// TableCellView renders columns/rows as an HTML <table> with
// monospace font and simple right/center alignment. Clicking a
// column header sorts — purely client-side so plugins don't need to
// ship sorted data.
const TableCellView: React.FC<{ cell: PluginCell }> = ({ cell }) => {
  const [sortCol, setSortCol] = useState<number | null>(null);
  const [sortAsc, setSortAsc] = useState(true);
  const cols = cell.columns || [];
  const rawRows = cell.rows || [];

  const sortedRows = useMemo(() => {
    if (sortCol == null) return rawRows;
    const asNum = (s: string) => {
      const n = parseFloat(s);
      return isNaN(n) ? null : n;
    };
    return [...rawRows].sort((a, b) => {
      const av = a[sortCol] ?? "";
      const bv = b[sortCol] ?? "";
      const an = asNum(av);
      const bn = asNum(bv);
      const cmp = an != null && bn != null ? an - bn : av.localeCompare(bv);
      return sortAsc ? cmp : -cmp;
    });
  }, [rawRows, sortCol, sortAsc]);

  if (cols.length === 0) {
    return <span style={{ color: colors.dimText }}>(empty table)</span>;
  }
  const headerCell = (col: TableColumn, i: number): React.CSSProperties => ({
    textAlign: (col.align as any) || "left",
    color: colors.cyan,
    cursor: "pointer",
    padding: "2px 6px",
    userSelect: "none",
    borderBottom: `1px solid ${colors.border}`,
  });
  const dataCell = (col: TableColumn): React.CSSProperties => ({
    textAlign: (col.align as any) || "left",
    color: colors.white,
    padding: "2px 6px",
  });

  return (
    <div style={{ display: "inline-block", maxHeight: 240, overflow: "auto" }}>
      {cell.label && <div style={{ color: colors.dimText, marginBottom: 2 }}>{cell.label}</div>}
      <table style={{ borderCollapse: "collapse", fontFamily: fonts.mono, fontSize: 11 }}>
        <thead>
          <tr>
            {cols.map((col, i) => (
              <th
                key={i}
                style={headerCell(col, i)}
                onClick={() => {
                  if (sortCol === i) setSortAsc(!sortAsc);
                  else {
                    setSortCol(i);
                    setSortAsc(true);
                  }
                }}
                title="click to sort"
              >
                {col.header}
                {sortCol === i ? (sortAsc ? " ▲" : " ▼") : ""}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sortedRows.map((row, r) => (
            <tr key={r}>
              {cols.map((col, c) => (
                <td key={c} style={dataCell(col)}>
                  {row[c] ?? ""}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export const CellGridPanel: React.FC<Props> = ({ label, cells, screenTopic, screenId }) => {
  const [editingKey, setEditingKey] = useState<string | null>(null);
  // pendingKeys tracks cells whose input we just dispatched but
  // haven't yet seen reflected in a plugin response. Without this
  // marker the user clicks a dropdown / submits a number and gets
  // zero feedback while the plugin recomputes — feels frozen even
  // though the request is in flight. Cleared on the next prop
  // update for `cells` (which is when the plugin's full_replace
  // lands on the SSE stream).
  const [pendingKeys, setPendingKeys] = useState<Set<string>>(() => new Set());
  // Latest `cells` that triggered a clear, so we don't clear on
  // re-renders unrelated to the plugin response.
  const lastCellsRef = useRef(cells);
  React.useEffect(() => {
    if (lastCellsRef.current !== cells && pendingKeys.size > 0) {
      setPendingKeys(new Set());
    }
    lastCellsRef.current = cells;
    // pendingKeys intentionally excluded from deps — including it
    // would re-run the effect after the clear and not after the
    // prop change, fighting React batching.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cells]);

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

  // Surface a Cancel button when the grid has a running progress
  // cell (component_id === "progress" with value < 1). Calls
  // POST /api/v1/plugin/cancel which routes through to the plugin's
  // input topic as an InputEvent{Kind:"cancel"}.
  const hasRunningJob = cells.some(
    (c) => c.component_id === "progress" && typeof c.value === "number" && c.value < 1 && c.value > 0,
  );

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.label}>{label}</span>
        <span style={s.cellCount}>{cells.length} cells</span>
        {pendingKeys.size > 0 && (
          <span style={s.pendingHint} title="Plugin is computing the new value">
            ⟳ pending {pendingKeys.size === 1 ? "" : `(${pendingKeys.size})`}
          </span>
        )}
        {hasRunningJob && (
          <button
            onClick={() => sendPluginCancel(screenTopic)}
            style={{
              marginLeft: "auto",
              padding: "2px 8px",
              background: colors.red,
              color: "#fff",
              border: "none",
              borderRadius: 3,
              cursor: "pointer",
              fontFamily: fonts.mono,
              fontSize: 11,
            }}
            title="Cancel the plugin's current job"
          >
            Cancel
          </button>
        )}
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
                const isPending = pendingKeys.has(key);
                tds.push(
                  <td
                    key={key}
                    style={{
                      ...s.td,
                      ...(isPending ? s.tdPending : {}),
                    }}
                    colSpan={span > 1 ? span : undefined}
                  >
                    <CellRenderer
                      cell={c}
                      editing={editingKey === key}
                      onStartEdit={() => setEditingKey(key)}
                      onSubmit={(value) => {
                        setEditingKey(null);
                        // Mark pending immediately so the next paint
                        // shows feedback — fire-and-forget the POST
                        // so the React thread never blocks on the
                        // round-trip. Cleared by the cells-prop
                        // useEffect when the plugin response lands.
                        setPendingKeys((prev) => {
                          const next = new Set(prev);
                          next.add(key);
                          return next;
                        });
                        sendPluginInput(screenTopic, screenId, c.address.row, c.address.col, value);
                      }}
                      onCancel={() => setEditingKey(null)}
                    />
                    {isPending && <span style={s.pendingDot}>⟳</span>}
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
  td: { padding: "2px 4px", verticalAlign: "top", borderBottom: `1px solid #111`, position: "relative" },
  tdPending: { background: "#1A1200", boxShadow: `inset 0 0 0 1px ${colors.amber}` },
  pendingDot: { position: "absolute", top: 2, right: 4, color: colors.amber, fontSize: 10, fontFamily: fonts.mono },
  pendingHint: { color: colors.amber, fontSize: 11, fontFamily: fonts.mono, marginLeft: 8 },
};
