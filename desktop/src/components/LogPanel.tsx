import React, { useRef, useEffect } from "react";
import { colors, fonts } from "../styles/theme";

interface Props { lines: string[]; }

export const LogPanel: React.FC<Props> = ({ lines }) => {
  const bottomRef = useRef<HTMLDivElement>(null);
  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: "auto" }); }, [lines]);

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.label}>SERVER LOG</span>
        <span style={s.count}>{lines.length} lines</span>
      </div>
      <div style={s.output}>
        {lines.length === 0 && <div style={{ color: colors.dimText }}>Waiting for server logs...</div>}
        {lines.map((line, i) => (
          <div key={i} style={{
            ...s.line,
            color: line.includes("[ERROR]") ? colors.red
              : line.includes("[WARN]") ? colors.amber
              : line.includes("[INFO]") ? colors.green
              : colors.dimText,
          }}>{line}</div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  container: { display: "flex", flexDirection: "column", height: "100%" },
  header: { display: "flex", alignItems: "center", gap: 12, padding: "6px 12px", background: "#0D0D0D", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 },
  label: { fontSize: 13, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  count: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono },
  output: { flex: 1, overflow: "auto", padding: "8px 12px", fontFamily: fonts.mono, fontSize: 11 },
  line: { whiteSpace: "pre-wrap" as const, lineHeight: 1.4 },
};
