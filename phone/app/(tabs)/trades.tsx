import React, { useState, useEffect, useMemo, useCallback, memo } from "react";
import { View, Text, FlatList, TouchableOpacity, StyleSheet, ScrollView, ListRenderItem } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { getServerUrl, getToken, onConnectionChange } from "../../src/connection";

interface TradeAgg {
  Instrument: string; Exchange: string;
  Count: number; Volume: number; BuyVolume: number; SellVolume: number;
  VWAP: number; Open: number; High: number; Low: number; Close: number;
  Turnover: number; P25: number; P50: number; P75: number;
}

interface TradeEntry {
  Price: number; Quantity: number; Side: string; Timestamp: string;
}

interface TradeSnap {
  Instrument: string; Exchange: string; Trades: TradeEntry[];
}

// Cap rendered trade rows so the main thread doesn't stall when the
// snapshot grows (ANR repro 2026-04-22 at ~104 trades/s).
const MAX_VISIBLE_TRADES = 30;

function useTradeData() {
  const [aggs, setAggs] = useState<Record<string, TradeAgg>>({});
  const [snaps, setSnaps] = useState<Record<string, TradeSnap>>({});
  const [keys, setKeys] = useState<string[]>([]);
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
          const aggResp = await fetch(
            `${url}/api/v1/snapshot?topic=trade.agg.*.*&mode=latest&token=${token}`
          );
          const aggData = await aggResp.json();
          if (active && Array.isArray(aggData)) {
            const newAggs: Record<string, TradeAgg> = {};
            const newKeys: string[] = [];
            for (const item of aggData) {
              if (item.Instrument && item.Exchange) {
                const key = `${item.Exchange}/${item.Instrument}`;
                newAggs[key] = item;
                newKeys.push(key);
              }
            }
            newKeys.sort();
            setAggs(newAggs);
            setKeys((prev) => shallowEqualArrays(prev, newKeys) ? prev : newKeys);
          }

          const snapResp = await fetch(
            `${url}/api/v1/snapshot?topic=trade.snap.*.*&mode=latest&token=${token}`
          );
          const snapData = await snapResp.json();
          if (active && Array.isArray(snapData)) {
            const newSnaps: Record<string, TradeSnap> = {};
            for (const item of snapData) {
              if (item.Instrument && item.Exchange) {
                const key = `${item.Exchange}/${item.Instrument}`;
                // Trim on ingest so downstream renders never see the
                // unbounded list.
                const trimmed = Array.isArray(item.Trades) && item.Trades.length > MAX_VISIBLE_TRADES
                  ? { ...item, Trades: item.Trades.slice(-MAX_VISIBLE_TRADES) }
                  : item;
                newSnaps[key] = trimmed;
              }
            }
            setSnaps(newSnaps);
          }
        } catch {}
        await new Promise((r) => setTimeout(r, 3000));
      }
    }

    poll();
    return () => { active = false; };
  }, [token]);

  return { aggs, snaps, keys };
}

function shallowEqualArrays(a: string[], b: string[]) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

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
  const { aggs, snaps, keys } = useTradeData();
  const [activeIdx, setActiveIdx] = useState(0);

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

  const activeKey = keys[activeIdx] || keys[0];
  const agg = aggs[activeKey];
  const snap = snaps[activeKey];

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
      <Text style={presets.header}>TRADES</Text>

      {keys.length > 1 && (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={s.selector}>
          {keys.map((k, i) => (
            <TouchableOpacity
              key={k}
              onPress={() => setActiveIdx(i)}
              style={[s.selectorBtn, i === activeIdx && s.selectorBtnActive]}
            >
              <Text style={[s.selectorText, i === activeIdx && s.selectorTextActive]}>
                {k.split("/")[1] || k}
              </Text>
            </TouchableOpacity>
          ))}
        </ScrollView>
      )}

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
  selectorBtnActive: { borderColor: colors.amber, backgroundColor: colors.amberDim },
  selectorText: {
    fontFamily: fonts.mono, fontWeight: "700", fontSize: 11,
    color: colors.muted, textTransform: "uppercase", letterSpacing: 0.5,
  },
  selectorTextActive: { color: colors.amber },
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
