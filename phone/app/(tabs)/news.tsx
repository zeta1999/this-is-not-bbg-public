import React, { useState, useEffect, useCallback } from "react";
import { View, Text, FlatList, TextInput, TouchableOpacity, StyleSheet, Linking, Modal, ScrollView } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { getServerUrl, getToken, onConnectionChange } from "../../src/connection";

interface NewsItem {
  id: string;
  title: string;
  body: string;
  source: string;
  tickers: string[];
  url: string;
  published: string;
}

function sleep(ms: number) { return new Promise((r) => setTimeout(r, ms)); }

function useNews() {
  const [items, setItems] = useState<NewsItem[]>([]);
  const [connected, setConnected] = useState(false);
  const [token, setToken] = useState(getToken());

  useEffect(() => {
    const unsub = onConnectionChange(() => setToken(getToken()));
    return unsub;
  }, []);

  useEffect(() => {
    if (!token) { setConnected(false); return; }
    let active = true;

    async function poll() {
      while (active) {
        try {
          const url = getServerUrl();
          const resp = await fetch(`${url}/api/v1/snapshot?topic=news&limit=200&token=${encodeURIComponent(token)}`);
          if (!resp.ok) { setConnected(false); await sleep(10000); continue; }
          const data = await resp.json();
          if (!Array.isArray(data)) { await sleep(10000); continue; }

          setConnected(true);
          const parsed: NewsItem[] = data.map((d: any) => ({
            id: d.ID || d.id || "",
            title: d.Title || d.title || "",
            body: d.Body || d.body || d.Description || d.description || "",
            source: d.Source || d.source || "",
            tickers: d.Tickers || d.tickers || [],
            url: d.URL || d.Url || d.url || "",
            published: d.Published || d.published || d.Timestamp || "",
          })).filter((n: NewsItem) => n.title);

          // Dedup by id or title, sort newest first.
          const seen = new Set<string>();
          const deduped: NewsItem[] = [];
          for (const n of parsed) {
            const key = n.id || n.title;
            if (!seen.has(key)) { seen.add(key); deduped.push(n); }
          }
          deduped.sort((a, b) => (b.published || "").localeCompare(a.published || ""));
          setItems(deduped);
        } catch {
          setConnected(false);
        }
        await sleep(15000);
      }
    }

    poll();
    return () => { active = false; };
  }, [token]);

  return { items, connected };
}

function timeAgo(published: string): string {
  if (!published) return "";
  const ts = new Date(published).getTime();
  if (isNaN(ts)) return "";
  const ago = Math.floor((Date.now() - ts) / 60000);
  if (ago < 1) return "now";
  if (ago < 60) return `${ago}m`;
  if (ago < 1440) return `${Math.floor(ago / 60)}h`;
  return `${Math.floor(ago / 1440)}d`;
}

export default function NewsScreen() {
  const { items, connected } = useNews();
  const [search, setSearch] = useState("");
  const [searchResults, setSearchResults] = useState<NewsItem[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [selected, setSelected] = useState<NewsItem | null>(null);

  const doSearch = useCallback(async (q: string) => {
    if (!q.trim()) { setSearchResults(null); return; }
    const token = getToken();
    if (!token) return;
    setSearching(true);
    try {
      const url = getServerUrl();
      const resp = await fetch(`${url}/api/v1/news/search?q=${encodeURIComponent(q)}&limit=100&token=${encodeURIComponent(token)}`);
      if (resp.ok) {
        const data = await resp.json();
        if (Array.isArray(data)) {
          setSearchResults(data.map((d: any) => ({
            id: d.ID || d.id || "",
            title: d.Title || d.title || "",
            body: d.Body || d.body || d.Description || "",
            source: d.Source || d.source || "",
            tickers: d.Tickers || d.tickers || [],
            url: d.URL || d.Url || d.url || "",
            published: d.Published || d.published || d.Timestamp || "",
          })).filter((n: NewsItem) => n.title));
        }
      }
    } catch {}
    setSearching(false);
  }, []);

  useEffect(() => {
    if (!search.trim()) { setSearchResults(null); return; }
    const timer = setTimeout(() => doSearch(search), 400);
    return () => clearTimeout(timer);
  }, [search, doSearch]);

  const displayed = searchResults || items;

  if (!connected && items.length === 0) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>NEWS</Text>
        <View style={s.connRow}>
          <View style={[s.dot, { backgroundColor: colors.red }]} />
          <Text style={s.connText}>DISCONNECTED — go to Settings to pair</Text>
        </View>
      </View>
    );
  }

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>NEWS — {displayed.length} items{searching ? " (searching...)" : ""}</Text>
      <TextInput
        style={presets.input}
        placeholder="Search (BM25)..."
        placeholderTextColor={colors.muted}
        value={search}
        onChangeText={setSearch}
      />

      {/* Article detail modal */}
      <Modal visible={!!selected} animationType="slide" transparent>
        <View style={s.modalOverlay}>
          <View style={s.modalContent}>
            <ScrollView style={s.modalScroll}>
              <View style={s.modalMeta}>
                <Text style={s.modalSource}>{selected?.source || ""}</Text>
                <Text style={s.modalTime}>{selected ? timeAgo(selected.published) : ""}</Text>
              </View>
              <Text style={s.modalTitle}>{selected?.title || ""}</Text>
              {(selected?.tickers?.length ?? 0) > 0 && (
                <View style={s.tickerRow}>
                  {selected!.tickers.map((t) => (
                    <View key={t} style={s.tickerBadge}>
                      <Text style={s.tickerText}>{t}</Text>
                    </View>
                  ))}
                </View>
              )}
              {selected?.body ? (
                <Text style={s.modalBody}>{selected.body}</Text>
              ) : (
                <Text style={s.modalBodyEmpty}>No article body available.</Text>
              )}
            </ScrollView>
            <View style={s.modalActions}>
              {selected?.url ? (
                <TouchableOpacity style={s.openBtn} onPress={() => Linking.openURL(selected!.url)}>
                  <Text style={s.openBtnText}>OPEN IN BROWSER</Text>
                </TouchableOpacity>
              ) : null}
              <TouchableOpacity style={s.closeBtn} onPress={() => setSelected(null)}>
                <Text style={s.closeBtnText}>CLOSE</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>

      <FlatList
        data={displayed}
        keyExtractor={(item, i) => item.id || `${item.title}-${i}`}
        renderItem={({ item }) => {
          const ago = timeAgo(item.published);
          return (
            <TouchableOpacity style={s.row} onPress={() => setSelected(item)}>
              <View style={s.meta}>
                <Text style={s.source}>{item.source}</Text>
                {ago ? <Text style={s.time}>{ago}</Text> : null}
              </View>
              <Text style={s.title} numberOfLines={2}>{item.title}</Text>
              {item.tickers.length > 0 && (
                <View style={s.tickerRow}>
                  {item.tickers.map((t) => (
                    <View key={t} style={s.tickerBadge}>
                      <Text style={s.tickerText}>{t}</Text>
                    </View>
                  ))}
                </View>
              )}
            </TouchableOpacity>
          );
        }}
        ItemSeparatorComponent={() => <View style={presets.divider} />}
      />
    </View>
  );
}

const s = StyleSheet.create({
  row: { paddingVertical: spacing.sm, paddingHorizontal: spacing.lg },
  meta: { flexDirection: "row", gap: 8, marginBottom: 4 },
  source: { fontFamily: fonts.mono, fontSize: 10, fontWeight: "700", color: colors.blue },
  time: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted },
  title: { fontFamily: fonts.mono, fontSize: 13, color: colors.textBright },
  tickerRow: { flexDirection: "row", gap: 4, marginTop: 4, flexWrap: "wrap" },
  tickerBadge: { backgroundColor: "#1A1200", paddingHorizontal: 6, paddingVertical: 2, borderRadius: 3 },
  tickerText: { fontFamily: fonts.mono, fontSize: 9, fontWeight: "700", color: colors.amber },
  connRow: { flexDirection: "row", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 6 },
  connText: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted },
  // Article detail modal
  modalOverlay: { flex: 1, backgroundColor: "rgba(0,0,0,0.9)" },
  modalContent: { flex: 1, marginTop: 40, backgroundColor: colors.bg, borderTopLeftRadius: 12, borderTopRightRadius: 12 },
  modalScroll: { flex: 1, padding: spacing.lg },
  modalMeta: { flexDirection: "row", gap: 8, marginBottom: 8 },
  modalSource: { fontFamily: fonts.mono, fontSize: 11, fontWeight: "700", color: colors.blue },
  modalTime: { fontFamily: fonts.mono, fontSize: 11, color: colors.muted },
  modalTitle: { fontFamily: fonts.mono, fontSize: 16, fontWeight: "900", color: colors.textBright, marginBottom: 12 },
  modalBody: { fontFamily: fonts.mono, fontSize: 13, color: colors.secondary, lineHeight: 20 },
  modalBodyEmpty: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted, fontStyle: "italic" },
  modalActions: { flexDirection: "row", gap: 8, padding: spacing.lg, borderTopWidth: 1, borderTopColor: colors.border },
  openBtn: { flex: 1, backgroundColor: colors.amber, borderRadius: 6, paddingVertical: 12, alignItems: "center" },
  openBtnText: { fontFamily: fonts.mono, fontSize: 12, fontWeight: "700", color: "#000" },
  closeBtn: { flex: 1, borderWidth: 1, borderColor: colors.border, borderRadius: 6, paddingVertical: 12, alignItems: "center" },
  closeBtnText: { fontFamily: fonts.mono, fontSize: 12, fontWeight: "700", color: colors.muted },
});
