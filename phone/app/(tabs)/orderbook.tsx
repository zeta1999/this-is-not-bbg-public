import React, { useState, useEffect } from "react";
import { View, Text, ScrollView, TextInput, TouchableOpacity, StyleSheet, Modal } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { getServerUrl, getToken, onConnectionChange } from "../../src/connection";

interface LOBLevel { Price: number; Quantity: number; }
interface LOBSnapshot {
  Instrument: string;
  Exchange: string;
  Bids: LOBLevel[];
  Asks: LOBLevel[];
}

function sleep(ms: number) { return new Promise((r) => setTimeout(r, ms)); }

function useLOB() {
  const [snapshots, setSnapshots] = useState<Map<string, LOBSnapshot>>(new Map());
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
          const resp = await fetch(`${url}/api/v1/snapshot?topic=lob.*.*&mode=latest&token=${encodeURIComponent(token)}`);
          if (!resp.ok) { setConnected(false); await sleep(5000); continue; }
          const data = await resp.json();
          if (!Array.isArray(data)) { await sleep(3000); continue; }

          setConnected(true);
          setSnapshots(() => {
            const m = new Map<string, LOBSnapshot>();
            for (const snap of data) {
              const key = `${snap.Instrument}/${snap.Exchange}`;
              m.set(key, snap);
            }
            return m;
          });
        } catch {
          setConnected(false);
        }
        await sleep(3000);
      }
    }

    poll();
    return () => { active = false; };
  }, [token]);

  return { snapshots, connected };
}

export default function OrderBookScreen() {
  const { snapshots, connected } = useLOB();
  const [activeKey, setActiveKey] = useState("");
  const [search, setSearch] = useState("");
  const [showPicker, setShowPicker] = useState(false);

  const keys = Array.from(snapshots.keys()).sort();

  // Auto-select first instrument if none selected.
  useEffect(() => {
    if (!activeKey && keys.length > 0) setActiveKey(keys[0]);
  }, [keys.length]);

  const snap = snapshots.get(activeKey);
  const bids = snap?.Bids?.slice(0, 20) || [];
  const asks = snap?.Asks?.slice(0, 20) || [];
  const spread = bids.length > 0 && asks.length > 0 ? asks[0].Price - bids[0].Price : 0;

  const filtered = keys.filter((k) => {
    if (!search) return true;
    const q = search.toLowerCase();
    const s = snapshots.get(k);
    return k.toLowerCase().includes(q) || (s?.Instrument || "").toLowerCase().includes(q) || (s?.Exchange || "").toLowerCase().includes(q);
  });

  const fmtPrice = (p: number) => {
    if (p >= 10000) return p.toFixed(1);
    if (p >= 100) return p.toFixed(2);
    if (p >= 1) return p.toFixed(3);
    return p.toFixed(6);
  };

  const fmtQty = (q: number) => {
    if (q >= 1000) return q.toFixed(1);
    if (q >= 1) return q.toFixed(3);
    return q.toFixed(5);
  };

  if (!connected) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>ORDER BOOK</Text>
        <View style={st.connRow}>
          <View style={[st.dot, { backgroundColor: colors.red }]} />
          <Text style={st.connText}>DISCONNECTED — go to Settings to pair</Text>
        </View>
      </View>
    );
  }

  if (keys.length === 0) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>ORDER BOOK</Text>
        <View style={st.connRow}>
          <View style={[st.dot, { backgroundColor: colors.green }]} />
          <Text style={st.connText}>CONNECTED — waiting for LOB data...</Text>
        </View>
      </View>
    );
  }

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>ORDER BOOK — {keys.length} instruments</Text>

      {/* Instrument selector — tap to open full-screen picker */}
      <TouchableOpacity style={st.selectedRow} onPress={() => setShowPicker(true)}>
        <Text style={st.selectedInst}>{snap?.Instrument || "—"}</Text>
        <Text style={st.selectedExch}>{snap?.Exchange || ""}</Text>
        <Text style={st.arrow}>▼</Text>
      </TouchableOpacity>

      {/* Full-screen modal picker */}
      <Modal visible={showPicker} animationType="slide" transparent>
        <View style={st.modalOverlay}>
          <View style={st.modalContent}>
            <View style={st.modalHeader}>
              <Text style={st.modalTitle}>SELECT INSTRUMENT</Text>
              <TouchableOpacity onPress={() => { setShowPicker(false); setSearch(""); }}>
                <Text style={st.modalClose}>✕</Text>
              </TouchableOpacity>
            </View>
            <TextInput
              style={st.searchInput}
              placeholder="Search..."
              placeholderTextColor={colors.muted}
              value={search}
              onChangeText={setSearch}
              autoFocus
            />
            <Text style={st.modalCount}>{filtered.length} instruments</Text>
            <ScrollView style={st.modalList} contentContainerStyle={st.modalListContent}>
              {filtered.map((key) => {
                const inst = snapshots.get(key);
                const isActive = key === activeKey;
                return (
                  <TouchableOpacity
                    key={key}
                    style={[st.modalItem, isActive && st.modalItemActive]}
                    onPress={() => { setActiveKey(key); setShowPicker(false); setSearch(""); }}>
                    <Text style={[st.modalName, isActive && { color: colors.amber }]}>{inst?.Instrument || key}</Text>
                    <Text style={st.modalExch}>{inst?.Exchange || ""}</Text>
                  </TouchableOpacity>
                );
              })}
            </ScrollView>
          </View>
        </View>
      </Modal>

      <Text style={st.spread}>Spread: ${fmtPrice(spread)}</Text>

      {/* Column headers */}
      <View style={st.headerRow}>
        <Text style={[st.colH, { color: colors.green }]}>QTY</Text>
        <Text style={[st.colH, { color: colors.green }]}>BID</Text>
        <Text style={[st.colH, { color: colors.red }]}>ASK</Text>
        <Text style={[st.colH, { color: colors.red }]}>QTY</Text>
      </View>

      <ScrollView>
        {Array.from({ length: Math.max(bids.length, asks.length) }).map((_, i) => (
          <View key={i} style={st.row}>
            <Text style={[st.cell, { color: colors.green }]}>{bids[i] ? fmtQty(bids[i].Quantity) : ""}</Text>
            <Text style={[st.cell, { color: colors.green, fontWeight: "700" }]}>{bids[i] ? fmtPrice(bids[i].Price) : ""}</Text>
            <Text style={[st.cell, { color: colors.red, fontWeight: "700" }]}>{asks[i] ? fmtPrice(asks[i].Price) : ""}</Text>
            <Text style={[st.cell, { color: colors.red }]}>{asks[i] ? fmtQty(asks[i].Quantity) : ""}</Text>
          </View>
        ))}
      </ScrollView>
    </View>
  );
}

const st = StyleSheet.create({
  selectedRow: { flexDirection: "row", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, marginBottom: spacing.xs },
  selectedInst: { fontFamily: fonts.mono, fontSize: 16, fontWeight: "900", color: colors.amber },
  selectedExch: { fontFamily: fonts.mono, fontSize: 11, color: colors.muted, marginLeft: 8 },
  arrow: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted, marginLeft: "auto" },
  spread: { fontFamily: fonts.mono, fontSize: 11, color: colors.amber, paddingHorizontal: spacing.lg, marginBottom: spacing.sm },
  headerRow: { flexDirection: "row", paddingVertical: 4, paddingHorizontal: spacing.lg, borderBottomWidth: 1, borderBottomColor: colors.border },
  colH: { flex: 1, fontFamily: fonts.mono, fontSize: 9, fontWeight: "700", textAlign: "center" },
  row: { flexDirection: "row", paddingVertical: 3, paddingHorizontal: spacing.lg },
  cell: { flex: 1, fontFamily: fonts.mono, fontSize: 12, textAlign: "center" },
  connRow: { flexDirection: "row", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 6 },
  connText: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted },
  // Modal picker
  modalOverlay: { flex: 1, backgroundColor: "rgba(0,0,0,0.85)", justifyContent: "center", padding: 20 },
  modalContent: { backgroundColor: colors.bg, borderWidth: 1, borderColor: colors.border, borderRadius: 8, maxHeight: "70%", minHeight: 300 },
  modalHeader: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderBottomWidth: 1, borderBottomColor: colors.border },
  modalTitle: { fontFamily: fonts.mono, fontSize: 12, fontWeight: "700", color: colors.amber, letterSpacing: 1 },
  modalClose: { fontSize: 18, color: colors.muted, padding: 4 },
  searchInput: { fontFamily: fonts.mono, fontSize: 13, color: colors.textBright, paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderBottomWidth: 1, borderBottomColor: colors.border },
  modalCount: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted, paddingHorizontal: spacing.lg, paddingVertical: 4 },
  modalList: { flexGrow: 1, flexShrink: 1 },
  modalListContent: { paddingBottom: 20 },
  modalItem: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: 10, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  modalItemActive: { backgroundColor: "#1A1200" },
  modalName: { fontFamily: fonts.mono, fontSize: 14, fontWeight: "700", color: colors.textBright },
  modalExch: { fontFamily: fonts.mono, fontSize: 11, color: colors.muted },
});
