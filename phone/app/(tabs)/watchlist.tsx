import React, { useState, useMemo, useEffect } from "react";
import { View, Text, FlatList, TextInput, StyleSheet } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { useStore } from "../../src/store";

export default function WatchlistScreen() {
  const { prices, priceKeys, connected, lastEventAt } = useStore();
  const [search, setSearch] = useState("");
  // Re-render the freshness indicator on a 1s tick so the user
  // sees the "live N s ago" counter advance even when no new data
  // has arrived. Decoupled from the SSE event path so quiet
  // markets don't appear frozen.
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const items = useMemo(() => {
    const all = priceKeys.map((k) => prices.get(k)!).filter(Boolean);
    const q = search.toLowerCase();
    return all
      .filter((p) => !q
        || p.instrument.toLowerCase().includes(q)
        || p.exchange.toLowerCase().includes(q))
      .sort((a, b) => a.instrument.localeCompare(b.instrument));
  }, [prices, priceKeys, search]);

  const fmtPrice = (p: number) => {
    if (p >= 10000) return `$${Math.round(p).toLocaleString()}`;
    if (p >= 100) return `$${p.toFixed(1)}`;
    if (p >= 1) return `$${p.toFixed(2)}`;
    return `$${p.toFixed(4)}`;
  };

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>WATCHLIST — {items.length} instruments</Text>
      <TextInput style={presets.input} placeholder="Search..." placeholderTextColor={colors.muted}
        value={search} onChangeText={setSearch} />
      <View style={s.connRow}>
        <View style={[s.dot, { backgroundColor: connected ? colors.green : colors.red }]} />
        <Text style={s.connText}>
          {connected
            ? `LIVE — last update ${lastEventAt ? Math.max(0, Math.floor((now - lastEventAt) / 1000)) + "s ago" : "—"}`
            : "DISCONNECTED — go to Settings to pair"}
        </Text>
      </View>
      <FlatList
        data={items}
        keyExtractor={(item) => `${item.instrument}/${item.exchange}`}
        renderItem={({ item }) => (
          <View style={s.row}>
            <View>
              <Text style={s.symbol}>{item.instrument}</Text>
              <Text style={s.exchange}>{item.exchange}</Text>
            </View>
            <View style={s.priceCol}>
              <Text style={[s.price, { color: item.change >= 0 ? colors.green : colors.red }]}>
                {fmtPrice(item.price)}
              </Text>
              <Text style={[s.change, { color: item.change >= 0 ? colors.green : colors.red }]}>
                {item.change >= 0 ? "+" : ""}{item.change.toFixed(2)}%
              </Text>
            </View>
          </View>
        )}
        ItemSeparatorComponent={() => <View style={presets.divider} />}
      />
    </View>
  );
}

const s = StyleSheet.create({
  row: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingVertical: spacing.sm, paddingHorizontal: spacing.lg },
  symbol: { fontFamily: fonts.mono, fontWeight: "700", fontSize: 14, color: colors.textBright },
  exchange: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted, marginTop: 2 },
  priceCol: { alignItems: "flex-end" },
  price: { fontFamily: fonts.mono, fontWeight: "700", fontSize: 14 },
  change: { fontFamily: fonts.mono, fontSize: 10, marginTop: 2 },
  connRow: { flexDirection: "row", alignItems: "center", paddingHorizontal: spacing.lg, paddingBottom: spacing.sm },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 6 },
  connText: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted },
});
