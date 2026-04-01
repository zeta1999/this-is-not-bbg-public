import React, { useState, useRef, useEffect } from "react";
import { colors, fonts } from "../styles/theme";

function getToken(): string {
  return new URLSearchParams(window.location.search).get("token") || "";
}

export const AgentPanel: React.FC = () => {
  const [lines, setLines] = useState<string[]>(["Type a message and press Enter to ask Claude."]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: "auto" }); }, [lines]);

  const handleSubmit = async () => {
    const prompt = input.trim();
    if (!prompt || loading) return;
    setInput("");
    setLines((prev) => [...prev, `$ ${prompt}`]);
    setLoading(true);

    try {
      const token = getToken();
      const resp = await fetch(`http://localhost:9474/api/v1/agent/exec?token=${token}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt }),
      });
      const data = await resp.json();
      const response = data.response || data.error || "No response";
      // Split into lines for display.
      setLines((prev) => [...prev, ...response.split("\n")]);
    } catch (e: any) {
      setLines((prev) => [...prev, `Error: ${e.message}`]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.label}>AGENT</span>
        <span style={s.count}>{lines.length} lines</span>
        {loading && <span style={s.loading}>⏳ thinking...</span>}
      </div>
      <div style={s.output}>
        {lines.map((line, i) => (
          <div key={i} style={{
            ...s.line,
            color: line.startsWith("$") ? colors.amber
              : line.startsWith("Error") ? colors.red
              : line.startsWith("#") ? colors.amber
              : colors.white,
          }}>{line}</div>
        ))}
        <div ref={bottomRef} />
      </div>
      <div style={s.inputRow}>
        <span style={s.prompt}>{'>'}</span>
        <input type="text" value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") handleSubmit(); }}
          placeholder={loading ? "Waiting for Claude..." : "Ask claude anything..."}
          disabled={loading} style={s.input} autoFocus />
      </div>
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  container: { display: "flex", flexDirection: "column", height: "100%" },
  header: { display: "flex", alignItems: "center", gap: 12, padding: "6px 12px", background: "#0D0D0D", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 },
  label: { fontSize: 13, fontWeight: 900, color: colors.amber, fontFamily: fonts.mono },
  count: { fontSize: 10, color: colors.dimText, fontFamily: fonts.mono },
  loading: { fontSize: 10, color: colors.amber, fontFamily: fonts.mono, marginLeft: "auto" },
  output: { flex: 1, overflow: "auto", padding: "8px 12px", fontFamily: fonts.mono, fontSize: 12 },
  line: { whiteSpace: "pre-wrap" as const, lineHeight: 1.5, wordBreak: "break-word" as const },
  inputRow: { display: "flex", alignItems: "center", padding: "8px 12px", borderTop: `1px solid ${colors.border}`, background: "#0D0D0D", flexShrink: 0 },
  prompt: { fontFamily: fonts.mono, fontSize: 14, color: colors.amber, marginRight: 8, fontWeight: 700 },
  input: { flex: 1, fontFamily: fonts.mono, fontSize: 12, padding: "6px 8px", background: colors.bg, border: `1px solid ${colors.border}`, color: colors.white, borderRadius: 2, outline: "none" },
};
