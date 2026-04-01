import React, { useState, useMemo, useEffect } from "react";
import { colors, fonts, panelStyle } from "../styles/theme";
import type { NewsItem } from "../store";

interface Props {
  items: NewsItem[];
}

export const NewsPanel: React.FC<Props> = ({ items }) => {
  const [search, setSearch] = useState("");
  const [selectedIdx, setSelectedIdx] = useState(-1);

  const [serverResults, setServerResults] = useState<NewsItem[]>([]);

  // Local filter.
  const localFiltered = useMemo(() => {
    if (!search) return items;
    const q = search.toLowerCase();
    return items.filter((n) =>
      n.title.toLowerCase().includes(q) ||
      n.source.toLowerCase().includes(q) ||
      n.tickers.some((t) => t.toLowerCase().includes(q))
    );
  }, [items, search]);

  // Server-side search when local results are few.
  useEffect(() => {
    if (!search || search.length < 2) { setServerResults([]); return; }
    const timeout = setTimeout(async () => {
      try {
        const token = new URLSearchParams(window.location.search).get("token") || "";
        const resp = await fetch(`http://localhost:9474/api/v1/news/search?q=${encodeURIComponent(search)}&limit=100&token=${token}`);
        const data = await resp.json();
        if (Array.isArray(data)) {
          setServerResults(data.map((p: any) => ({
            title: p.Title || "", source: p.Source || "", body: p.Body || "",
            url: p.URL || "", tickers: p.Tickers || [], timestamp: Date.now() / 1000,
          })));
        }
      } catch { setServerResults([]); }
    }, 500); // debounce
    return () => clearTimeout(timeout);
  }, [search]);

  // Merge local + server results, dedup by title.
  const filtered = useMemo(() => {
    if (!search) return items;
    const seen = new Set<string>();
    const merged: NewsItem[] = [];
    for (const n of [...localFiltered, ...serverResults]) {
      if (!seen.has(n.title)) { seen.add(n.title); merged.push(n); }
    }
    return merged;
  }, [items, localFiltered, serverResults, search]);

  const selected = selectedIdx >= 0 && selectedIdx < filtered.length ? filtered[selectedIdx] : null;

  if (selected) {
    return (
      <div style={{ ...panelStyle, display: "flex", flexDirection: "column", padding: 0 }}>
        <div style={styles.header}>
          <button style={styles.backBtn} onClick={() => setSelectedIdx(-1)}>← Back</button>
          <span style={styles.label}>ARTICLE</span>
        </div>
        <div style={styles.article}>
          <div style={styles.articleTitle}>{selected.title}</div>
          <div style={styles.articleMeta}>
            <span style={styles.source}>{selected.source}</span>
            {selected.tickers.map((t) => <span key={t} style={styles.ticker}>{t}</span>)}
          </div>
          {selected.url && <div style={styles.articleUrl}>{selected.url}</div>}
          <div style={styles.articleBody}>{selected.body || "(no body)"}</div>
        </div>
      </div>
    );
  }

  return (
    <div style={{ ...panelStyle, display: "flex", flexDirection: "column", padding: 0 }}>
      <div style={styles.header}>
        <span style={styles.label}>NEWS</span>
        <span style={styles.count}>{filtered.length} items</span>
        <input type="text" placeholder="Search..." value={search}
          onChange={(e) => { setSearch(e.target.value); setSelectedIdx(-1); }}
          style={styles.searchInput} />
      </div>
      <div style={styles.list}>
        {filtered.length === 0 && <div style={styles.empty}>Waiting for news...</div>}
        {filtered.map((item, i) => {
          const ago = Math.floor((Date.now() / 1000 - item.timestamp) / 60);
          const agoStr = ago < 60 ? `${ago}m` : `${Math.floor(ago / 60)}h`;
          return (
            <div key={i} onClick={() => setSelectedIdx(i)} style={styles.row}>
              <span style={styles.source}>{item.source}</span>
              <span style={styles.time}>{agoStr}</span>
              <span style={styles.title}>{item.title}</span>
              {item.tickers.map((t) => <span key={t} style={styles.ticker}>{t}</span>)}
            </div>
          );
        })}
      </div>
    </div>
  );
};

const styles: Record<string, React.CSSProperties> = {
  header: { display: "flex", alignItems: "center", gap: 12, padding: "8px 12px", borderBottom: `1px solid ${colors.border}`, background: colors.surface, flexShrink: 0 },
  label: { fontFamily: fonts.mono, fontSize: 13, fontWeight: 900, color: colors.amber },
  count: { fontFamily: fonts.mono, fontSize: 10, color: colors.dimText },
  searchInput: { marginLeft: "auto", fontFamily: fonts.mono, fontSize: 11, padding: "3px 8px", background: colors.bg, border: `1px solid ${colors.border}`, color: colors.white, borderRadius: 2, width: 200, outline: "none" },
  backBtn: { fontFamily: fonts.mono, fontSize: 11, background: "none", border: `1px solid ${colors.border}`, color: colors.amber, padding: "2px 8px", cursor: "pointer", borderRadius: 2 },
  list: { flex: 1, overflow: "auto" },
  empty: { padding: 16, color: colors.dimText, fontFamily: fonts.mono, fontSize: 12 },
  row: { display: "flex", alignItems: "center", gap: 8, padding: "5px 12px", borderBottom: `1px solid ${colors.border}`, fontFamily: fonts.mono, fontSize: 11, cursor: "pointer" },
  source: { color: "#4488FF", fontWeight: 700, minWidth: 100, fontSize: 11 },
  time: { color: colors.dimText, minWidth: 35, fontSize: 10 },
  title: { color: colors.white, flex: 1 },
  ticker: { background: "#1A1200", color: colors.amber, padding: "1px 5px", borderRadius: 2, fontSize: 9, fontWeight: 700 },
  article: { flex: 1, overflow: "auto", padding: 16 },
  articleTitle: { fontFamily: fonts.mono, fontSize: 16, fontWeight: 900, color: colors.amber, marginBottom: 8 },
  articleMeta: { display: "flex", gap: 8, marginBottom: 8, alignItems: "center" },
  articleUrl: { fontFamily: fonts.mono, fontSize: 10, color: colors.dimText, marginBottom: 12 },
  articleBody: { fontFamily: fonts.mono, fontSize: 12, color: colors.white, lineHeight: 1.6 },
};
