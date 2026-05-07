// scan-qr.tsx — full-screen camera that decodes the {"url","token"}
// JSON the desktop PairModal and TUI /PAIR overlay both encode.
// On scan, persists the connection via setConnection() and pops
// back to Settings so the user can complete the PAIR flow.
//
// Permission flow follows the expo-camera 16 convention: ask
// inline, show a denied state when refused, allow manual back-out
// via the X button. We never auto-redirect away on denial — the
// user explicitly opted into this screen.

import React, { useCallback, useState } from "react";
import {
  View,
  Text,
  TouchableOpacity,
  StyleSheet,
  ActivityIndicator,
} from "react-native";
import { CameraView, useCameraPermissions } from "expo-camera";
import { useRouter } from "expo-router";
import { colors, fonts, spacing, presets } from "../src/theme";
import { setConnection } from "../src/connection";

export default function ScanQRScreen() {
  const router = useRouter();
  const [permission, requestPermission] = useCameraPermissions();
  const [scanned, setScanned] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleBarcode = useCallback(
    ({ data }: { data: string }) => {
      if (scanned) return;
      setScanned(true);
      try {
        const obj = JSON.parse(data);
        const url = typeof obj.url === "string" ? obj.url : "";
        const token = typeof obj.token === "string" ? obj.token : "";
        if (!url || !token) {
          setError("QR missing url or token field — re-run PAIR on the server.");
          setScanned(false);
          return;
        }
        setConnection(url, token);
        router.replace("/(tabs)/settings?scanned=1");
      } catch (e: any) {
        setError(`Could not parse QR: ${e.message}`);
        setScanned(false);
      }
    },
    [scanned, router],
  );

  if (!permission) {
    return (
      <View style={s.center}>
        <ActivityIndicator color={colors.amber} />
      </View>
    );
  }

  if (!permission.granted) {
    return (
      <View style={s.center}>
        <Text style={s.title}>CAMERA ACCESS</Text>
        <Text style={s.body}>
          The scanner needs camera access to read the pairing QR code shown by
          the desktop PairModal or the TUI /PAIR command.
        </Text>
        <TouchableOpacity style={presets.button} onPress={requestPermission}>
          <Text style={presets.buttonText}>GRANT ACCESS</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[presets.button, { marginTop: spacing.sm, backgroundColor: "transparent", borderWidth: 1, borderColor: colors.muted }]}
          onPress={() => router.back()}
        >
          <Text style={[presets.buttonText, { color: colors.muted }]}>BACK</Text>
        </TouchableOpacity>
      </View>
    );
  }

  return (
    <View style={s.fill}>
      <CameraView
        style={StyleSheet.absoluteFillObject}
        facing="back"
        onBarcodeScanned={scanned ? undefined : handleBarcode}
        barcodeScannerSettings={{ barcodeTypes: ["qr"] }}
      />
      <View style={s.overlay}>
        <Text style={s.hint}>Point the camera at the pairing QR.</Text>
        {error && <Text style={s.error}>{error}</Text>}
      </View>
      <View style={s.topBar}>
        <TouchableOpacity onPress={() => router.back()} style={s.closeBtn}>
          <Text style={s.closeText}>✕</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  fill: { flex: 1, backgroundColor: "#000" },
  center: { flex: 1, alignItems: "center", justifyContent: "center", padding: spacing.lg, backgroundColor: colors.bg },
  title: { fontFamily: fonts.mono, fontSize: 14, fontWeight: "900", color: colors.amber, marginBottom: spacing.md, letterSpacing: 1 },
  body: { fontFamily: fonts.mono, fontSize: 12, color: colors.text, textAlign: "center", marginBottom: spacing.lg, lineHeight: 18 },
  topBar: { position: "absolute", top: 50, left: 16, right: 16, flexDirection: "row" },
  closeBtn: { width: 36, height: 36, borderRadius: 18, backgroundColor: "rgba(0,0,0,0.55)", alignItems: "center", justifyContent: "center" },
  closeText: { color: "#fff", fontSize: 18, fontFamily: fonts.mono, fontWeight: "700" },
  overlay: { position: "absolute", left: 0, right: 0, bottom: 80, alignItems: "center", paddingHorizontal: 24 },
  hint: { fontFamily: fonts.mono, fontSize: 12, color: "#fff", backgroundColor: "rgba(0,0,0,0.5)", paddingHorizontal: 14, paddingVertical: 8, borderRadius: 6 },
  error: { fontFamily: fonts.mono, fontSize: 11, color: colors.red, marginTop: 12, backgroundColor: "rgba(0,0,0,0.6)", paddingHorizontal: 12, paddingVertical: 6, borderRadius: 6 },
});
