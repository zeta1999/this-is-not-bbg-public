package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// SanityVenue is one venue's quote in a SanitySnapshot. Mirrors the
// JSON shape published by server/internal/monitor/sanity.go on the
// `sanity.prices` topic, so the TUI can decode the same payload the
// desktop and phone apps consume.
type SanityVenue struct {
	Exchange   string  `json:"exchange"`
	Mid        float64 `json:"mid"`
	DeltaPct   float64 `json:"delta_pct"`
	AgeSeconds float64 `json:"age_seconds"`
	Outlier    bool    `json:"outlier"`
}

// SanitySnapshot is the cross-venue median + per-venue mids for a
// single instrument. LastUpdate is set by the TUI on receipt for the
// "Ns ago" age display, mirroring the desktop store.
type SanitySnapshot struct {
	Instrument   string        `json:"instrument"`
	Median       float64       `json:"median"`
	ThresholdPct float64       `json:"threshold_pct"`
	VenueCount   int           `json:"venue_count"`
	OutlierCount int           `json:"outlier_count"`
	Venues       []SanityVenue `json:"venues"`
	Timestamp    string        `json:"timestamp"` // RFC3339 from server
	LastUpdate   time.Time     `json:"-"`         // local clock, set on receipt
}

// RenderSanity renders one card per instrument with the cross-venue
// median, each venue's mid + delta, and outlier flags. Mirrors the
// desktop SanityPanel and phone sanity tab so the operator sees the
// same shape on every surface.
func RenderSanity(snapshots map[string]*SanitySnapshot, width, height int) string {
	header := amberStyle.Render("  CROSS-VENUE SANITY")
	hint := dimStyle.Render(
		"  median of mids · flags any venue beyond threshold or stale",
	)

	if len(snapshots) == 0 {
		return header + "\n" + hint + "\n\n  " + dimStyle.Render(
			"Waiting for sanity.prices — configure alerts.sanity_pairs on the server.",
		)
	}

	keys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	count := dimStyle.Render(fmt.Sprintf("  %d pair%s",
		len(keys), pluralS(len(keys))))

	now := time.Now()
	var cards []string
	for _, k := range keys {
		s := snapshots[k]
		if s == nil {
			continue
		}
		cards = append(cards, renderSanityCard(s, now, width))
	}

	return header + count + "\n" + hint + "\n\n" + strings.Join(cards, "\n\n")
}

func renderSanityCard(s *SanitySnapshot, now time.Time, width int) string {
	var ageSec float64
	if !s.LastUpdate.IsZero() {
		ageSec = now.Sub(s.LastUpdate).Seconds()
	}
	headerStale := ageSec > 60

	headerLine := amberStyle.Bold(true).Render(fmt.Sprintf("  %s", s.Instrument))
	headerLine += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Render(
		fmt.Sprintf("median %s", formatPrice(s.Median)),
	)

	if s.OutlierCount > 0 {
		headerLine += "  " + redStyle.Bold(true).Render(fmt.Sprintf(
			"%d OUTLIER%s", s.OutlierCount, pluralS(s.OutlierCount),
		))
	}

	ageStyle := dimStyle
	if headerStale {
		ageStyle = redStyle
	}
	headerLine += "  " + ageStyle.Render(formatAgo(time.Duration(ageSec*float64(time.Second))))

	var rows []string
	rows = append(rows, headerLine)

	if len(s.Venues) == 0 {
		rows = append(rows, dimStyle.Render("    no venues with fresh data"))
	}
	for _, v := range s.Venues {
		venueColor := lipgloss.Color("#4488FF")
		nameStyle := lipgloss.NewStyle().Foreground(venueColor).Bold(true)
		deltaStyle := dimStyle
		if absFloat(v.DeltaPct) > s.ThresholdPct {
			deltaStyle = redStyle
		}
		flag := ""
		if v.Outlier {
			flag = "  " + redStyle.Bold(true).Render("FLAG")
		}
		ageDur := time.Duration(v.AgeSeconds * float64(time.Second))
		rows = append(rows, fmt.Sprintf("    %s  %s  %s  %s%s",
			nameStyle.Render(fmt.Sprintf("%-10s", v.Exchange)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Render(
				fmt.Sprintf("%-12s", formatPrice(v.Mid)),
			),
			deltaStyle.Render(fmt.Sprintf("%+7.3f%%", v.DeltaPct)),
			dimStyle.Render(formatAgo(ageDur)),
			flag,
		))
	}

	rows = append(rows, dimStyle.Render(fmt.Sprintf(
		"    threshold ±%.2f%% · %d venue%s",
		s.ThresholdPct, s.VenueCount, pluralS(s.VenueCount),
	)))

	return strings.Join(rows, "\n")
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
