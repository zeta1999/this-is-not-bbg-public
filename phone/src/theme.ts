import { StyleSheet, TextStyle, ViewStyle } from "react-native";

// ---------------------------------------------------------------------------
// Bloomberg-style dark theme constants
// ---------------------------------------------------------------------------

export const colors = {
  /** Primary background */
  bg: "#000000",
  /** Slightly elevated surfaces (cards, modals) */
  surface: "#0A0A0A",
  /** Card / list item background */
  card: "#111111",
  /** Subtle borders and dividers */
  border: "#1A1A1A",
  /** Muted / disabled text */
  muted: "#555555",
  /** Secondary text */
  secondary: "#888888",
  /** Primary text */
  text: "#CCCCCC",
  /** Bright / high-emphasis text */
  textBright: "#FFFFFF",

  /** Bloomberg amber accent */
  amber: "#FF8C00",
  /** Amber dimmed for backgrounds / badges */
  amberDim: "#332200",

  /** Positive / gain */
  green: "#00C853",
  /** Negative / loss */
  red: "#FF1744",
  /** Warning / triggered */
  yellow: "#FFD600",
  /** Info */
  blue: "#2979FF",
} as const;

export const fonts = {
  mono: "Courier",
  monoWeight: "700" as TextStyle["fontWeight"],
  sansSerif: "System",
} as const;

export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
} as const;

// ---------------------------------------------------------------------------
// Reusable style presets
// ---------------------------------------------------------------------------

const baseText: TextStyle = {
  color: colors.text,
  fontSize: 14,
};

const monoText: TextStyle = {
  ...baseText,
  fontFamily: fonts.mono,
  fontWeight: fonts.monoWeight,
};

export const presets = StyleSheet.create({
  /** Full-screen container */
  screen: {
    flex: 1,
    backgroundColor: colors.bg,
  } as ViewStyle,

  /** Row with space-between */
  row: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  } as ViewStyle,

  /** Card-like container */
  card: {
    backgroundColor: colors.card,
    borderRadius: 6,
    padding: spacing.md,
    marginHorizontal: spacing.lg,
    marginVertical: spacing.xs,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  } as ViewStyle,

  /** Standard body text */
  text: baseText,

  /** Monospace text (prices, symbols) */
  mono: monoText,

  /** Section header */
  header: {
    color: colors.amber,
    fontSize: 11,
    fontFamily: fonts.mono,
    fontWeight: "700",
    textTransform: "uppercase",
    letterSpacing: 1.2,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.sm,
  } as TextStyle,

  /** Large price display */
  priceDisplay: {
    fontFamily: fonts.mono,
    fontWeight: "700",
    fontSize: 18,
  } as TextStyle,

  /** Touchable list item - minimum 44pt touch target */
  touchable: {
    minHeight: 44,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.lg,
    justifyContent: "center",
  } as ViewStyle,

  /** Divider line */
  divider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.border,
  } as ViewStyle,

  /** Search / text input */
  input: {
    backgroundColor: colors.card,
    color: colors.textBright,
    fontFamily: fonts.mono,
    fontSize: 14,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: 6,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    marginHorizontal: spacing.lg,
    marginVertical: spacing.sm,
  } as TextStyle,

  /** Primary button */
  button: {
    backgroundColor: colors.amber,
    borderRadius: 6,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.xl,
    alignItems: "center",
    justifyContent: "center",
    minHeight: 48,
  } as ViewStyle,

  /** Button text */
  buttonText: {
    color: colors.bg,
    fontFamily: fonts.mono,
    fontWeight: "700",
    fontSize: 14,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  } as TextStyle,
});
