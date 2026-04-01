// ---------------------------------------------------------------------------
// Bloomberg-style color palette
// ---------------------------------------------------------------------------

export const colors = {
  /** Primary accent - amber / orange */
  amber: "#FF8C00",
  /** Positive / bid */
  green: "#00FF00",
  /** Negative / ask */
  red: "#FF4444",
  /** Main background */
  bg: "#000000",
  /** Elevated surface */
  surface: "#111111",
  /** Card / panel background */
  panel: "#0A0A0A",
  /** Dimmed text */
  dimText: "#666666",
  /** Border color */
  border: "#222222",
  /** Muted amber for less prominent elements */
  amberDim: "#CC7000",
  /** White text for high-contrast items */
  white: "#EEEEEE",
  /** Warning yellow */
  yellow: "#FFD700",
  /** Cyan accent */
  cyan: "#00BFFF",
} as const;

export const fonts = {
  mono: '"SF Mono", "Fira Code", "Cascadia Code", "Consolas", "Monaco", monospace',
} as const;

// Reusable style fragments
export const panelStyle: React.CSSProperties = {
  background: colors.surface,
  border: `1px solid ${colors.border}`,
  borderRadius: 2,
  padding: 12,
  height: "100%",
  overflow: "auto",
};

export const tableHeaderStyle: React.CSSProperties = {
  color: colors.amber,
  fontFamily: fonts.mono,
  fontSize: 11,
  fontWeight: 700,
  textTransform: "uppercase",
  letterSpacing: "0.05em",
  padding: "6px 10px",
  borderBottom: `1px solid ${colors.border}`,
  textAlign: "left",
};

export const tableCellStyle: React.CSSProperties = {
  fontFamily: fonts.mono,
  fontSize: 12,
  padding: "5px 10px",
  borderBottom: `1px solid ${colors.border}`,
  color: colors.white,
};
