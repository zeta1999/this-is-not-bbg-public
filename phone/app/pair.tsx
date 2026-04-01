import React, { useState, useRef } from "react";
import {
  View,
  Text,
  StyleSheet,
  Pressable,
  ActivityIndicator,
  TextInput,
} from "react-native";
import { CameraView, useCameraPermissions } from "expo-camera";
import { useRouter } from "expo-router";
import { pair } from "../src/api/client";
import { PairPayload } from "../src/types";
import { colors, fonts, spacing, presets } from "../src/theme";

export default function PairScreen() {
  const router = useRouter();
  const [permission, requestPermission] = useCameraPermissions();
  const [error, setError] = useState<string | null>(null);
  const [isPairing, setIsPairing] = useState(false);
  const [showManual, setShowManual] = useState(false);
  const [manualInput, setManualInput] = useState("");
  const hasScanned = useRef(false);

  // ---------------------------------------------------------------------------
  // Parse QR payload: expects JSON { host, port, token }
  // ---------------------------------------------------------------------------
  function parseQRPayload(data: string): PairPayload | null {
    try {
      const parsed = JSON.parse(data);
      if (
        typeof parsed.host === "string" &&
        typeof parsed.port === "number" &&
        typeof parsed.token === "string"
      ) {
        return parsed as PairPayload;
      }
    } catch {
      // not JSON
    }

    // Also accept "host:port:token" format
    const parts = data.split(":");
    if (parts.length >= 3) {
      const port = parseInt(parts[1], 10);
      if (!isNaN(port)) {
        return {
          host: parts[0],
          port,
          token: parts.slice(2).join(":"),
        };
      }
    }

    return null;
  }

  // ---------------------------------------------------------------------------
  // Handle a scanned code
  // ---------------------------------------------------------------------------
  async function handleBarCodeScanned(data: string) {
    if (hasScanned.current || isPairing) return;
    hasScanned.current = true;

    const payload = parseQRPayload(data);
    if (!payload) {
      setError("Invalid QR code. Expected server connection info.");
      hasScanned.current = false;
      return;
    }

    setError(null);
    setIsPairing(true);

    try {
      await pair(payload);
      router.replace("/(tabs)/watchlist");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Pairing failed");
      hasScanned.current = false;
    } finally {
      setIsPairing(false);
    }
  }

  // ---------------------------------------------------------------------------
  // Manual entry submit
  // ---------------------------------------------------------------------------
  async function handleManualSubmit() {
    const trimmed = manualInput.trim();
    if (!trimmed) return;
    await handleBarCodeScanned(trimmed);
  }

  // ---------------------------------------------------------------------------
  // Permission not yet determined
  // ---------------------------------------------------------------------------
  if (!permission) {
    return (
      <View style={[presets.screen, styles.center]}>
        <ActivityIndicator color={colors.amber} size="large" />
      </View>
    );
  }

  // ---------------------------------------------------------------------------
  // Permission denied
  // ---------------------------------------------------------------------------
  if (!permission.granted) {
    return (
      <View style={[presets.screen, styles.center]}>
        <Text style={styles.title}>CAMERA ACCESS REQUIRED</Text>
        <Text style={styles.subtitle}>
          Grant camera permission to scan the pairing QR code from your server
          terminal.
        </Text>
        <Pressable style={presets.button} onPress={requestPermission}>
          <Text style={presets.buttonText}>GRANT PERMISSION</Text>
        </Pressable>
        <Pressable
          style={styles.manualLink}
          onPress={() => setShowManual(true)}
        >
          <Text style={styles.manualLinkText}>Enter code manually</Text>
        </Pressable>
      </View>
    );
  }

  // ---------------------------------------------------------------------------
  // Manual entry mode
  // ---------------------------------------------------------------------------
  if (showManual) {
    return (
      <View style={[presets.screen, styles.center]}>
        <Text style={styles.title}>MANUAL PAIRING</Text>
        <Text style={styles.subtitle}>
          Paste the connection string from your server terminal.
        </Text>
        <Text style={styles.formatHint}>
          {'Format: {"host":"...","port":...,"token":"..."}'}
        </Text>

        <TextInput
          style={[presets.input, styles.manualInput]}
          placeholder='{"host":"192.168.1.10","port":9090,"token":"abc123"}'
          placeholderTextColor={colors.muted}
          value={manualInput}
          onChangeText={setManualInput}
          multiline
          autoCapitalize="none"
          autoCorrect={false}
        />

        {error && <Text style={styles.error}>{error}</Text>}

        <View style={styles.manualActions}>
          <Pressable
            style={presets.button}
            onPress={handleManualSubmit}
            disabled={isPairing}
          >
            {isPairing ? (
              <ActivityIndicator color={colors.bg} />
            ) : (
              <Text style={presets.buttonText}>CONNECT</Text>
            )}
          </Pressable>
          <Pressable
            style={styles.manualLink}
            onPress={() => {
              setShowManual(false);
              setError(null);
            }}
          >
            <Text style={styles.manualLinkText}>Scan QR instead</Text>
          </Pressable>
        </View>
      </View>
    );
  }

  // ---------------------------------------------------------------------------
  // Camera scanner
  // ---------------------------------------------------------------------------
  return (
    <View style={presets.screen}>
      <View style={styles.cameraContainer}>
        <CameraView
          style={styles.camera}
          facing="back"
          barcodeScannerSettings={{
            barcodeTypes: ["qr"],
          }}
          onBarcodeScanned={({ data }) => handleBarCodeScanned(data)}
        />

        {/* Overlay with scanning frame */}
        <View style={styles.overlay}>
          <View style={styles.overlayTop} />
          <View style={styles.overlayMiddle}>
            <View style={styles.overlaySide} />
            <View style={styles.scanFrame}>
              <View style={[styles.corner, styles.cornerTL]} />
              <View style={[styles.corner, styles.cornerTR]} />
              <View style={[styles.corner, styles.cornerBL]} />
              <View style={[styles.corner, styles.cornerBR]} />
            </View>
            <View style={styles.overlaySide} />
          </View>
          <View style={styles.overlayBottom}>
            <Text style={styles.title}>PAIR WITH SERVER</Text>
            <Text style={styles.subtitle}>
              Scan the QR code displayed on your notbbg terminal
            </Text>

            {isPairing && (
              <View style={styles.statusRow}>
                <ActivityIndicator color={colors.amber} />
                <Text style={styles.statusText}>Connecting...</Text>
              </View>
            )}

            {error && <Text style={styles.error}>{error}</Text>}

            <Pressable
              style={styles.manualLink}
              onPress={() => setShowManual(true)}
            >
              <Text style={styles.manualLinkText}>Enter code manually</Text>
            </Pressable>
          </View>
        </View>
      </View>
    </View>
  );
}

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------
const SCAN_SIZE = 250;

const styles = StyleSheet.create({
  center: {
    justifyContent: "center",
    alignItems: "center",
    padding: spacing.xl,
  },
  title: {
    fontFamily: fonts.mono,
    fontWeight: "700",
    fontSize: 18,
    color: colors.amber,
    letterSpacing: 1.5,
    textAlign: "center",
    marginBottom: spacing.sm,
  },
  subtitle: {
    fontFamily: fonts.sansSerif,
    fontSize: 14,
    color: colors.secondary,
    textAlign: "center",
    lineHeight: 20,
    marginBottom: spacing.xl,
    maxWidth: 280,
  },
  formatHint: {
    fontFamily: fonts.mono,
    fontSize: 11,
    color: colors.muted,
    textAlign: "center",
    marginBottom: spacing.lg,
  },

  // Camera
  cameraContainer: {
    flex: 1,
  },
  camera: {
    flex: 1,
  },

  // Overlay
  overlay: {
    ...StyleSheet.absoluteFillObject,
  },
  overlayTop: {
    flex: 1,
    backgroundColor: "rgba(0,0,0,0.7)",
  },
  overlayMiddle: {
    flexDirection: "row",
    height: SCAN_SIZE,
  },
  overlaySide: {
    flex: 1,
    backgroundColor: "rgba(0,0,0,0.7)",
  },
  scanFrame: {
    width: SCAN_SIZE,
    height: SCAN_SIZE,
  },
  overlayBottom: {
    flex: 1,
    backgroundColor: "rgba(0,0,0,0.7)",
    alignItems: "center",
    paddingTop: spacing.xxl,
  },

  // Corner marks on scan frame
  corner: {
    position: "absolute",
    width: 24,
    height: 24,
    borderColor: colors.amber,
  },
  cornerTL: {
    top: 0,
    left: 0,
    borderTopWidth: 3,
    borderLeftWidth: 3,
  },
  cornerTR: {
    top: 0,
    right: 0,
    borderTopWidth: 3,
    borderRightWidth: 3,
  },
  cornerBL: {
    bottom: 0,
    left: 0,
    borderBottomWidth: 3,
    borderLeftWidth: 3,
  },
  cornerBR: {
    bottom: 0,
    right: 0,
    borderBottomWidth: 3,
    borderRightWidth: 3,
  },

  // Status
  statusRow: {
    flexDirection: "row",
    alignItems: "center",
    marginTop: spacing.md,
    gap: spacing.sm,
  },
  statusText: {
    fontFamily: fonts.mono,
    fontSize: 13,
    color: colors.amber,
  },
  error: {
    fontFamily: fonts.mono,
    fontSize: 12,
    color: colors.red,
    textAlign: "center",
    marginTop: spacing.md,
    paddingHorizontal: spacing.xl,
  },

  // Manual entry link
  manualLink: {
    marginTop: spacing.lg,
    padding: spacing.md,
  },
  manualLinkText: {
    fontFamily: fonts.mono,
    fontSize: 12,
    color: colors.amber,
    textDecorationLine: "underline",
  },

  // Manual input
  manualInput: {
    height: 80,
    textAlignVertical: "top",
    width: "100%",
  },
  manualActions: {
    alignItems: "center",
    width: "100%",
    paddingHorizontal: spacing.lg,
    marginTop: spacing.md,
  },
});
