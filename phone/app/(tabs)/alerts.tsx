import React from "react";
import { View, Text, FlatList, StyleSheet } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { useStore, type AlertEntry } from "../../src/store";

const SEV_COLOR: Record<string, string> = {
  critical: colors.red,
  warning: colors.amber,
  warn: colors.amber,
  info: colors.muted,
};

function fmtTime(ts: number): string {
  const ago = Math.floor((Date.now() - ts) / 1000);
  if (ago < 60) return `${ago}s ago`;
  if (ago < 3600) return `${Math.floor(ago / 60)}m ago`;
  return `${Math.floor(ago / 3600)}h ago`;
}

export default function AlertsScreen() {
  const { alerts, connected } = useStore();

  if (!connected) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>ALERTS</Text>
        <View style={s.empty}>
          <Text style={s.text}>DISCONNECTED — go to Settings to pair</Text>
        </View>
      </View>
    );
  }

  if (alerts.length === 0) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>ALERTS</Text>
        <View style={s.empty}>
          <Text style={s.text}>No alerts yet.</Text>
          <Text style={s.text}>Phone is read-only — create alerts from the TUI:</Text>
          <Text style={s.code}>/ALERT SET BTCUSDT {">"} 100000</Text>
        </View>
      </View>
    );
  }

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>ALERTS — {alerts.length}</Text>
      <FlatList
        data={alerts}
        keyExtractor={(a: AlertEntry) => a.id}
        renderItem={({ item }) => (
          <View style={s.row}>
            <Text style={[s.sev, { color: SEV_COLOR[item.severity] || colors.muted }]}>
              {item.severity.toUpperCase()}
            </Text>
            <Text style={s.message}>{item.message}</Text>
            <Text style={s.time}>{fmtTime(item.timestamp)}</Text>
          </View>
        )}
        ItemSeparatorComponent={() => <View style={presets.divider} />}
      />
    </View>
  );
}

const s = StyleSheet.create({
  empty: { padding: spacing.lg },
  text: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted, marginBottom: spacing.sm },
  code: { fontFamily: fonts.mono, fontSize: 12, color: colors.amber, marginTop: spacing.sm },
  row: { paddingVertical: spacing.sm, paddingHorizontal: spacing.lg, flexDirection: "row", alignItems: "flex-start", gap: 8 },
  sev: { fontFamily: fonts.mono, fontSize: 9, fontWeight: "700", width: 60, marginTop: 2 },
  message: { fontFamily: fonts.mono, fontSize: 12, color: colors.text, flex: 1 },
  time: { fontFamily: fonts.mono, fontSize: 9, color: colors.muted, marginTop: 2 },
});
