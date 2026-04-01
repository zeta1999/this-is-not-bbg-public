import React, { useState, useEffect } from "react";
import { View, Text, FlatList, TextInput, StyleSheet } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { getServerUrl, getToken, onConnectionChange } from "../../src/connection";

interface PriceEntry {
  instrument: string;
  exchange: string;
  price: number;
  change: number;
}

function sleep(ms: number) { return new Promise((r) => setTimeout(r, ms)); }

// Poll /api/v1/snapshot for OHLC data (React Native has no EventSource).
function usePrices() {
  const [prices, setPrices] = useState<Map<string, PriceEntry>>(new Map());
  const [connected, setConnected] = useState(false);
  const [token, setToken] = useState(getToken());

  // Re-read token when connection changes (after pairing in Settings).
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
          const resp = await fetch(`${url}/api/v1/snapshot?topic=ohlc.*.*&mode=latest&token=${encodeURIComponent(token)}`);
          if (!resp.ok) { setConnected(false); await sleep(5000); continue; }
          const data = await resp.json();
          if (!Array.isArray(data)) { await sleep(3000); continue; }

          setConnected(true);
          setPrices((prev) => {
            const m = new Map(prev);
            for (const p of data) {
              const key = `${p.Instrument}/${p.Exchange}`;
              const old = m.get(key);
              const change = old && old.price > 0 ? ((p.Close - old.price) / old.price * 100) : 0;
              m.set(key, { instrument: p.Instrument, exchange: p.Exchange, price: p.Close, change });
            }
            return m;
          });
        } catch {
          setConnected(false);
        }
        await sleep(5000);
      }
    }

    poll();
    return () => { active = false; };
  }, [token]);

  return { prices, connected };
}

export default function WatchlistScreen() {
  const { prices, connected } = usePrices();
  const [search, setSearch] = useState("");

  const items = Array.from(prices.values())
    .filter((p) => !search || p.instrument.toLowerCase().includes(search.toLowerCase()) || p.exchange.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => a.instrument.localeCompare(b.instrument));

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
        <Text style={s.connText}>{connected ? "CONNECTED" : "DISCONNECTED — go to Settings to pair"}</Text>
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
