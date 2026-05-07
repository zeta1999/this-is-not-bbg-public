import React, { useState, useMemo, useCallback, memo } from "react";
import { View, Text, FlatList, TouchableOpacity, StyleSheet, ScrollView, ListRenderItem, Modal, TextInput } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { useStore, type TradeEntry } from "../../src/store";

// Cap rendered trade rows so the main thread doesn't stall when the
// snapshot grows (ANR repro 2026-04-22 at ~104 trades/s).
const MAX_VISIBLE_TRADES = 30;

interface TradeRowProps { trade: TradeEntry }

const TradeRow = memo(({ trade }: TradeRowProps) => (
  <View style={s.tradeRow}>
    <Text style={[s.tradeCell, { color: trade.Side === "buy" ? colors.green : colors.red, fontWeight: "700" }]}>
      {trade.Side.toUpperCase()}
    </Text>
    <Text style={s.tradeCell}>{trade.Price.toFixed(2)}</Text>
    <Text style={s.tradeCell}>{trade.Quantity.toFixed(6)}</Text>
    <Text style={[s.tradeCell, { color: colors.muted }]}>
      {trade.Timestamp ? new Date(trade.Timestamp).toLocaleTimeString() : ""}
    </Text>
  </View>
));

export default function TradesScreen() {
  const { tradeAggs: aggs, tradeSnaps: snaps, tradeKeys: keys } = useStore();
  const [activeKey, setActiveKey] = useState("");
  const [showPicker, setShowPicker] = useState(false);
  const [search, setSearch] = useState("");

  // Auto-select the first key once data starts arriving. Keeps the
  // active selection stable across re-renders (was previously
  // index-based, which could silently drift when keys re-sort).
  React.useEffect(() => {
    if (!activeKey && keys.length > 0) setActiveKey(keys[0]);
  }, [keys.length, activeKey]);

  if (keys.length === 0) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>TRADES</Text>
        <View style={s.empty}>
          <Text style={s.emptyText}>Waiting for trade data...</Text>
        </View>
      </View>
    );
  }

  const resolvedKey = activeKey || keys[0];
  const agg = aggs[resolvedKey];
  const snap = snaps[resolvedKey];

  // key format: "<exchange>/<instrument>". Surface both halves so
  // the picker shows ticker AND venue, mirroring the LOB screen.
  const splitKey = (k: string): { exchange: string; instrument: string } => {
    const i = k.indexOf("/");
    if (i < 0) return { exchange: "", instrument: k };
    return { exchange: k.slice(0, i), instrument: k.slice(i + 1) };
  };
  const active = splitKey(resolvedKey);

  const filtered = keys.filter((k) => {
    if (!search) return true;
    const q = search.toLowerCase();
    const { exchange, instrument } = splitKey(k);
    return instrument.toLowerCase().includes(q) || exchange.toLowerCase().includes(q) || k.toLowerCase().includes(q);
  });

  // Reverse once per snap change; bounded by MAX_VISIBLE_TRADES from
  // the fetch pipeline, so this stays cheap.
  const reversed = useMemo(() => {
    if (!snap || !snap.Trades) return [];
    const out = snap.Trades.slice(-MAX_VISIBLE_TRADES);
    out.reverse();
    return out;
  }, [snap]);

  const buyPct = agg && agg.Volume > 0 ? (agg.BuyVolume / agg.Volume * 100) : 0;

  const renderRow: ListRenderItem<TradeEntry> = useCallback(({ item }) => (
    <TradeRow trade={item} />
  ), []);

  const keyExtractor = useCallback((_: TradeEntry, i: number) => String(i), []);

  const header = (
    <View>
      {agg && (
        <View style={s.statsBlock}>
          <View style={s.statRow}>
            <View style={s.stat}>
              <Text style={s.statLabel}>VWAP</Text>
              <Text style={[s.statValue, { color: "#00BFFF" }]}>{agg.VWAP.toFixed(2)}</Text>
            </View>
            <View style={s.stat}>
              <Text style={s.statLabel}>Volume</Text>
              <Text style={s.statValue}>{agg.Volume.toFixed(4)}</Text>
            </View>
            <View style={s.stat}>
              <Text style={s.statLabel}>Trades/s</Text>
              <Text style={s.statValue}>{agg.Count}</Text>
            </View>
          </View>
          <View style={s.statRow}>
            <View style={s.stat}>
              <Text style={s.statLabel}>High</Text>
              <Text style={[s.statValue, { color: colors.green }]}>{agg.High.toFixed(2)}</Text>
            </View>
            <View style={s.stat}>
              <Text style={s.statLabel}>Low</Text>
              <Text style={[s.statValue, { color: colors.red }]}>{agg.Low.toFixed(2)}</Text>
            </View>
            <View style={s.stat}>
              <Text style={s.statLabel}>Buy%</Text>
              <Text style={[s.statValue, { color: colors.green }]}>{buyPct.toFixed(0)}%</Text>
            </View>
          </View>
        </View>
      )}
      {reversed.length > 0 && (
        <Text style={s.sectionHeader}>RECENT TRADES</Text>
      )}
    </View>
  );

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>TRADES — {keys.length} feeds</Text>

      {/* Instrument + venue picker — same modal pattern as LOB so a
          BTCUSDT subscription on three exchanges shows up as three
          rows, not collapsed onto a single "BTCUSDT" button. */}
      <TouchableOpacity style={s.selectedRow} onPress={() => setShowPicker(true)}>
        <Text style={s.selectedInst}>{active.instrument || "—"}</Text>
        <Text style={s.selectedExch}>{active.exchange || ""}</Text>
        <Text style={s.arrow}>▼</Text>
      </TouchableOpacity>

      <Modal visible={showPicker} animationType="slide" transparent>
        <View style={s.modalOverlay}>
          <View style={s.modalContent}>
            <View style={s.modalHeader}>
              <Text style={s.modalTitle}>SELECT FEED</Text>
              <TouchableOpacity onPress={() => { setShowPicker(false); setSearch(""); }}>
                <Text style={s.modalClose}>✕</Text>
              </TouchableOpacity>
            </View>
            <TextInput
              style={s.searchInput}
              placeholder="Search ticker or venue..."
              placeholderTextColor={colors.muted}
              value={search}
              onChangeText={setSearch}
              autoFocus
            />
            <Text style={s.modalCount}>{filtered.length} feeds</Text>
            <ScrollView style={s.modalList} contentContainerStyle={s.modalListContent}>
              {filtered.map((k) => {
                const { exchange, instrument } = splitKey(k);
                const isActive = k === resolvedKey;
                return (
                  <TouchableOpacity
                    key={k}
                    style={[s.modalItem, isActive && s.modalItemActive]}
                    onPress={() => { setActiveKey(k); setShowPicker(false); setSearch(""); }}>
                    <Text style={[s.modalName, isActive && { color: colors.amber }]}>{instrument}</Text>
                    <Text style={s.modalExch}>{exchange}</Text>
                  </TouchableOpacity>
                );
              })}
            </ScrollView>
          </View>
        </View>
      </Modal>

      {/* FlatList virtualizes the rows so even a hot symbol with many
          trades no longer stalls the JS thread. */}
      <FlatList
        style={{ flex: 1 }}
        contentContainerStyle={s.content}
        data={reversed}
        renderItem={renderRow}
        keyExtractor={keyExtractor}
        ListHeaderComponent={header}
        initialNumToRender={12}
        maxToRenderPerBatch={12}
        windowSize={5}
        removeClippedSubviews
      />
    </View>
  );
}

const s = StyleSheet.create({
  empty: { padding: spacing.lg },
  emptyText: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted },
  // Selected feed row — tap to open the modal.
  selectedRow: { flexDirection: "row", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, marginBottom: spacing.xs },
  selectedInst: { fontFamily: fonts.mono, fontSize: 16, fontWeight: "900", color: colors.amber },
  selectedExch: { fontFamily: fonts.mono, fontSize: 11, color: colors.muted, marginLeft: 8 },
  arrow: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted, marginLeft: "auto" },
  // Modal picker — mirrors LOB visual language.
  modalOverlay: { flex: 1, backgroundColor: "rgba(0,0,0,0.85)", justifyContent: "center", padding: 20 },
  modalContent: { backgroundColor: colors.bg, borderWidth: 1, borderColor: colors.border, borderRadius: 8, maxHeight: "70%", minHeight: 300 },
  modalHeader: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderBottomWidth: 1, borderBottomColor: colors.border },
  modalTitle: { fontFamily: fonts.mono, fontSize: 12, fontWeight: "700", color: colors.amber, letterSpacing: 1 },
  modalClose: { fontSize: 18, color: colors.muted, padding: 4 },
  searchInput: { fontFamily: fonts.mono, fontSize: 13, color: colors.text, paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderBottomWidth: 1, borderBottomColor: colors.border },
  modalCount: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted, paddingHorizontal: spacing.lg, paddingVertical: 4 },
  modalList: { flexGrow: 1, flexShrink: 1 },
  modalListContent: { paddingBottom: 20 },
  modalItem: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: 10, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  modalItemActive: { backgroundColor: "#1A1200" },
  modalName: { fontFamily: fonts.mono, fontSize: 14, fontWeight: "700", color: colors.text },
  modalExch: { fontFamily: fonts.mono, fontSize: 11, color: colors.muted },
  content: { padding: spacing.md },
  statsBlock: { marginBottom: 12 },
  statRow: { flexDirection: "row", marginBottom: 8 },
  stat: { flex: 1, paddingRight: 8 },
  statLabel: {
    fontFamily: fonts.mono, fontSize: 9, fontWeight: "700",
    color: colors.muted, textTransform: "uppercase", letterSpacing: 0.5,
  },
  statValue: { fontFamily: fonts.mono, fontSize: 14, fontWeight: "700", color: colors.text },
  sectionHeader: {
    fontFamily: fonts.mono, fontSize: 11, fontWeight: "900",
    color: colors.amber, marginBottom: 6,
  },
  tradeRow: { flexDirection: "row", paddingVertical: 3, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  tradeCell: { flex: 1, fontFamily: fonts.mono, fontSize: 12, color: colors.text },
});
