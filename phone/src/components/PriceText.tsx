import React from "react";
import { Text, TextStyle, StyleSheet } from "react-native";
import { colors, fonts } from "../theme";

interface PriceTextProps {
  value: number;
  /** Number of decimal places (default 2) */
  decimals?: number;
  /** If true, prefix with + or - sign */
  showSign?: boolean;
  /** If true, append "%" */
  isPercent?: boolean;
  /** Override font size */
  size?: number;
  style?: TextStyle;
}

export function PriceText({
  value,
  decimals = 2,
  showSign = false,
  isPercent = false,
  size,
  style,
}: PriceTextProps) {
  const isPositive = value > 0;
  const isNegative = value < 0;
  const color = isPositive
    ? colors.green
    : isNegative
      ? colors.red
      : colors.text;

  const sign = showSign && isPositive ? "+" : "";
  const formatted = `${sign}${value.toFixed(decimals)}${isPercent ? "%" : ""}`;

  return (
    <Text
      style={[
        styles.base,
        { color },
        size != null && { fontSize: size },
        style,
      ]}
      numberOfLines={1}
    >
      {formatted}
    </Text>
  );
}

const styles = StyleSheet.create({
  base: {
    fontFamily: fonts.mono,
    fontWeight: "700",
    fontSize: 14,
  },
});
