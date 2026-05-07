// Cross-venue sanity tab — mirrors desktop SanityPanel and TUI's
// future SANITY view. Subscribes to `sanity.prices` via the shared
// store; renders one card per pair with the median + per-venue
// delta and an outlier flag when any venue diverges past the
// configured threshold (set on the server via alerts.sanity_*).
import React, { useMemo, useState, useEffect } from "react";
import { View, Text, ScrollView, StyleSheet } from "react-native";
import { colors, fonts, spacing, presets } from "../../src/theme";
import { useStore, type SanitySnapshot, type SanityVenue } from "../../src/store";

function fmtPrice(p: number): string {
  if (!isFinite(p)) return "—";
  if (p >= 10000) return `$${Math.round(p).toLocaleString()}`;
  if (p >= 100) return `$${p.toFixed(2)}`;
  if (p >= 1) return `$${p.toFixed(3)}`;
  return `$${p.toFixed(5)}`;
}

function fmtAge(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return "—";
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h`;
}

export default function SanityScreen() {
  const { sanitySnapshots, connected } = useStore();
  // Force re-render on a 1s tick so the per-card "Ns ago" badge
  // advances even when no new sanity event has arrived.
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const rows = useMemo(() => {
    return Object.values(sanitySnapshots).sort((a, b) =>
      a.instrument.localeCompare(b.instrument)
    );
  }, [sanitySnapshots]);

  if (!connected) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>SANITY</Text>
        <View style={s.connRow}>
          <View style={[s.dot, { backgroundColor: colors.red }]} />
          <Text style={s.connText}>DISCONNECTED — go to Settings to pair</Text>
        </View>
      </View>
    );
  }

  if (rows.length === 0) {
    return (
      <View style={presets.screen}>
        <Text style={presets.header}>SANITY</Text>
        <View style={s.empty}>
          <Text style={s.emptyText}>Waiting for sanity.prices…</Text>
          <Text style={s.emptyHint}>
            Configure alerts.sanity_pairs on the server and ensure at least
            one venue is publishing OHLC/LOB.
          </Text>
        </View>
      </View>
    );
  }

  return (
    <View style={presets.screen}>
      <Text style={presets.header}>SANITY — {rows.length} pairs</Text>
      <Text style={s.hint}>median of mids · flags any venue beyond threshold or stale</Text>
      <ScrollView contentContainerStyle={s.list}>
        {rows.map((snap: SanitySnapshot) => {
          const ageSec = Math.max(0, (now - snap.lastUpdate) / 1000);
          const headerStale = ageSec > 60;
          const hasOutlier = snap.outlier_count > 0;
          return (
            <View
              key={snap.instrument}
              style={[s.card, hasOutlier && { borderColor: colors.red }]}
            >
              <View style={[s.cardHeader, hasOutlier && { backgroundColor: "#2A0000" }]}>
                <Text style={s.instrument}>{snap.instrument}</Text>
                <Text style={s.median}>median {fmtPrice(snap.median)}</Text>
                {hasOutlier && (
                  <Text style={s.outlierBadge}>
                    {snap.outlier_count} OUTLIER{snap.outlier_count === 1 ? "" : "S"}
                  </Text>
                )}
                <Text style={[s.age, { color: headerStale ? colors.red : colors.muted }]}>
                  {fmtAge(ageSec)} ago
                </Text>
              </View>
              <View style={s.venueList}>
                {snap.venues.length === 0 && (
                  <Text style={s.emptyHint}>no venues with fresh data</Text>
                )}
                {snap.venues.map((v: SanityVenue) => {
                  const deltaColor =
                    Math.abs(v.delta_pct) > snap.threshold_pct ? colors.red : colors.muted;
                  return (
                    <View
                      key={v.exchange}
                      style={[s.venue, v.outlier && { backgroundColor: "#2A0000" }]}
                    >
                      <Text style={s.venueName}>{v.exchange}</Text>
                      <Text style={s.venuePrice}>{fmtPrice(v.mid)}</Text>
                      <Text style={[s.venueDelta, { color: deltaColor }]}>
                        {v.delta_pct >= 0 ? "+" : ""}
                        {v.delta_pct.toFixed(3)}%
                      </Text>
                      <Text style={s.venueAge}>{fmtAge(v.age_seconds)}</Text>
                      {v.outlier && <Text style={s.flagBadge}>FLAG</Text>}
                    </View>
                  );
                })}
              </View>
            </View>
          );
        })}
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  hint: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted, paddingHorizontal: spacing.lg, paddingBottom: spacing.sm },
  list: { padding: spacing.md, gap: spacing.sm },
  card: { borderWidth: 1, borderColor: colors.border, borderRadius: 6, marginBottom: spacing.sm },
  cardHeader: { flexDirection: "row", alignItems: "center", paddingVertical: spacing.sm, paddingHorizontal: spacing.md, gap: 8, borderBottomWidth: 1, borderBottomColor: colors.border },
  instrument: { fontFamily: fonts.mono, fontWeight: "900", fontSize: 14, color: colors.amber, flex: 1 },
  median: { fontFamily: fonts.mono, fontSize: 11, color: colors.text },
  outlierBadge: { fontFamily: fonts.mono, fontWeight: "700", fontSize: 9, color: colors.red, borderColor: colors.red, borderWidth: 1, paddingHorizontal: 6, paddingVertical: 2, borderRadius: 3 },
  age: { fontFamily: fonts.mono, fontSize: 10 },
  venueList: { paddingVertical: 4 },
  venue: { flexDirection: "row", alignItems: "center", paddingVertical: 6, paddingHorizontal: spacing.md, gap: 6 },
  venueName: { fontFamily: fonts.mono, fontSize: 11, fontWeight: "700", color: colors.text, width: 80 },
  venuePrice: { fontFamily: fonts.mono, fontSize: 11, color: colors.text, flex: 1, textAlign: "right" },
  venueDelta: { fontFamily: fonts.mono, fontSize: 10, width: 60, textAlign: "right" },
  venueAge: { fontFamily: fonts.mono, fontSize: 9, color: colors.muted, width: 30, textAlign: "right" },
  flagBadge: { fontFamily: fonts.mono, fontWeight: "700", fontSize: 8, color: colors.red, borderColor: colors.red, borderWidth: 1, paddingHorizontal: 4, paddingVertical: 1, borderRadius: 2, marginLeft: 4 },
  empty: { padding: spacing.lg },
  emptyText: { fontFamily: fonts.mono, fontSize: 12, color: colors.muted, marginBottom: 8 },
  emptyHint: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted },
  connRow: { flexDirection: "row", alignItems: "center", paddingHorizontal: spacing.lg, paddingVertical: spacing.sm },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 6 },
  connText: { fontFamily: fonts.mono, fontSize: 10, color: colors.muted },
});
