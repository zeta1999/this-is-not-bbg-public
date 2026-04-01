import React, { useState } from "react";
import { View, Text, TextInput, TouchableOpacity, StyleSheet, Alert, Platform } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { setConnection } from "../../src/connection";

// Default server URL — Android emulator uses 10.0.2.2 for host localhost.
const DEFAULT_URL = Platform.OS === "android" ? "http://10.0.2.2:9474" : "http://localhost:9474";

export default function SettingsScreen() {
  const [serverUrl, setServerUrl] = useState(DEFAULT_URL);
  const [token, setToken] = useState("");
  const [status, setStatus] = useState("Not connected");

  const testConnection = async () => {
    try {
      setStatus("Testing...");
      const resp = await fetch(`${serverUrl}/api/v1/health`);
      if (resp.ok) {
        const data = await resp.json();
        setStatus(`Connected — ${data.time}`);
      } else {
        setStatus(`Error: HTTP ${resp.status}`);
      }
    } catch (e: any) {
      setStatus(`Failed: ${e.message}`);
    }
  };

  const pair = async () => {
    if (!token) {
      Alert.alert("Token required", "Paste the token from /tmp/notbbg-phone.token on your server.");
      return;
    }
    try {
      setStatus("Pairing...");
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 5000);
      // Verify token works by hitting a protected endpoint.
      const resp = await fetch(`${serverUrl}/api/v1/snapshot?topic=ohlc.*.*&limit=1&token=${encodeURIComponent(token)}`, {
        signal: controller.signal,
      });
      clearTimeout(timeout);
      if (resp.ok) {
        setConnection(serverUrl, token);
        setStatus("Paired! Switch to Watchlist tab.");
      } else {
        const text = await resp.text();
        setStatus(`Pair failed: ${text}`);
      }
    } catch (e: any) {
      setStatus(`Pair failed: ${e.message}`);
    }
  };

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>CONNECTION</Text>

      <View style={s.field}>
        <Text style={s.label}>SERVER URL</Text>
        <TextInput style={presets.input} value={serverUrl} onChangeText={setServerUrl}
          placeholder="http://10.0.2.2:9474" placeholderTextColor={colors.muted}
          autoCapitalize="none" autoCorrect={false} />
      </View>

      <View style={s.field}>
        <Text style={s.label}>TOKEN</Text>
        <TextInput style={presets.input} value={token} onChangeText={setToken}
          placeholder="Paste from /tmp/notbbg-phone.token" placeholderTextColor={colors.muted}
          autoCapitalize="none" autoCorrect={false} secureTextEntry />
        <Text style={s.hint}>
          Get token: cat /tmp/notbbg-phone.token
        </Text>
      </View>

      <View style={s.buttons}>
        <TouchableOpacity style={presets.button} onPress={testConnection}>
          <Text style={presets.buttonText}>TEST</Text>
        </TouchableOpacity>
        <TouchableOpacity style={[presets.button, { marginLeft: spacing.sm }]} onPress={pair}>
          <Text style={presets.buttonText}>PAIR</Text>
        </TouchableOpacity>
      </View>

      <View style={s.statusBox}>
        <Text style={s.statusText}>{status}</Text>
      </View>

      <View style={presets.divider} />

      <View style={s.field}>
        <Text style={presets.header}>INFO</Text>
        <Text style={s.hint}>notbbg v0.2.0 — read-only mode</Text>
        <Text style={s.hint}>Token stored in secure storage after pairing.</Text>
        <Text style={s.hint}>Android emulator: use 10.0.2.2 for host localhost.</Text>
        <Text style={s.hint}>iOS simulator: use localhost directly.</Text>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  field: { paddingHorizontal: spacing.lg, marginBottom: spacing.md },
  label: { fontFamily: fonts.mono, fontSize: 10, fontWeight: "700", color: colors.amber, letterSpacing: 1, marginBottom: spacing.xs },
  hint: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted, marginTop: spacing.xs, paddingHorizontal: spacing.lg },
  buttons: { flexDirection: "row", paddingHorizontal: spacing.lg, marginBottom: spacing.md },
  statusBox: { paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, marginBottom: spacing.md },
  statusText: { fontFamily: fonts.mono, fontSize: 12, color: colors.green },
});
