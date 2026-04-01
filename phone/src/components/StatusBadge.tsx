import React from "react";
import { View, Text, StyleSheet, ViewStyle } from "react-native";
import { colors, fonts, spacing } from "../theme";

type BadgeVariant = "active" | "triggered" | "dismissed" | "ok" | "degraded" | "down" | "error";

const VARIANT_COLORS: Record<BadgeVariant, { bg: string; fg: string }> = {
  active: { bg: "#003D1A", fg: colors.green },
  triggered: { bg: "#332D00", fg: colors.yellow },
  dismissed: { bg: "#1A1A1A", fg: colors.muted },
  ok: { bg: "#003D1A", fg: colors.green },
  degraded: { bg: "#332D00", fg: colors.yellow },
  down: { bg: "#330A11", fg: colors.red },
  error: { bg: "#330A11", fg: colors.red },
};

interface StatusBadgeProps {
  label: string;
  variant: BadgeVariant;
  style?: ViewStyle;
}

export function StatusBadge({ label, variant, style }: StatusBadgeProps) {
  const palette = VARIANT_COLORS[variant] ?? VARIANT_COLORS.dismissed;

  return (
    <View style={[styles.badge, { backgroundColor: palette.bg }, style]}>
      <View style={[styles.dot, { backgroundColor: palette.fg }]} />
      <Text style={[styles.label, { color: palette.fg }]}>{label}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    borderRadius: 4,
    alignSelf: "flex-start",
  },
  dot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    marginRight: spacing.xs,
  },
  label: {
    fontFamily: fonts.mono,
    fontWeight: "700",
    fontSize: 10,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
});
