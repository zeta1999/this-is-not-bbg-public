import React from "react";
import { View, Text, StyleSheet } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";

export default function AlertsScreen() {
  return (
    <View style={presets.screen}>
      <Text style={presets.header}>ALERTS</Text>
      <View style={s.empty}>
        <Text style={s.text}>Alerts are read-only on the phone app.</Text>
        <Text style={s.text}>Create alerts from the TUI:</Text>
        <Text style={s.code}>/ALERT SET BTCUSDT {">"} 100000</Text>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  empty: { padding: spacing.lg },
  text: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted, marginBottom: spacing.sm },
  code: { fontFamily: fonts.mono, fontSize: 12, color: colors.amber, marginTop: spacing.sm },
});
