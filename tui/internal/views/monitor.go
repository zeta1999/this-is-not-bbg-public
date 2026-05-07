package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// FeedStatusEntry represents a feed's health for display.
type FeedStatusEntry struct {
	Name       string
	State      string
	LastUpdate time.Time
	LatencyMs  float64
	ErrorCount uint64
}

// BusStatsEntry is a snapshot of bus health rendered at the top of
// the MON panel. Populated by the TUI from the `bus.stats` topic.
type BusStatsEntry struct {
	Subscribers int
	Topics      int
	Dropped     uint64
	LastUpdate  time.Time
}

// WALStatsEntry summarizes one disk-spill WAL (cache or datalake).
// Populated from `wal.cache.stats` / `wal.datalake.stats`.
type WALStatsEntry struct {
	Name        string
	Written     int64
	Enqueued    uint64
	BusDropped  uint64
	WALDropped  uint64
	BytesOnDisk int64
	Segments    int
	LastUpdate  time.Time
}

// PluginStatusEntry represents a plugin process's health for display.
type PluginStatusEntry struct {
	Name         string
	State        string
	LastActivity time.Time
	PID          int
	ErrorCount   uint64
	Running      bool
}

// RenderMonitor renders the feed + plugin health monitoring panel.
// Back-compat signature — call RenderMonitorFull to include bus +
// WAL sections.
func RenderMonitor(feeds []FeedStatusEntry, plugins []PluginStatusEntry, width, height int) string {
	return RenderMonitorFull(feeds, plugins, BusStatsEntry{}, nil, width, height)
}

// RenderMonitorFull renders feeds, plugins, bus health, and WAL
// stats. Empty BusStatsEntry / nil WAL slice are treated as "no
// data yet" and hidden.
func RenderMonitorFull(feeds []FeedStatusEntry, plugins []PluginStatusEntry, busStats BusStatsEntry, wals []WALStatsEntry, width, height int) string {
	var sections []string

	// Bus + WAL section (compact row; shown only when we've seen
	// at least one stats frame).
	if !busStats.LastUpdate.IsZero() || len(wals) > 0 {
		header := amberStyle.Render("  BACKPRESSURE")
		var lines []string
		if !busStats.LastUpdate.IsZero() {
			lines = append(lines, fmt.Sprintf(
				"  %s subs=%-4d topics=%-4d dropped=%-8d  %s",
				greenStyle.Render("bus"),
				busStats.Subscribers,
				busStats.Topics,
				busStats.Dropped,
				dimStyle.Render(formatAgo(time.Since(busStats.LastUpdate))),
			))
		}
		for _, w := range wals {
			dropStyle := dimStyle
			if w.BusDropped > 0 || w.WALDropped > 0 {
				dropStyle = redStyle
			}
			lines = append(lines, fmt.Sprintf(
				"  %-12s bus_drop=%s  wal_drop=%s  bytes=%-9s segs=%-3d  %s",
				greenStyle.Render(fmt.Sprintf("wal.%s", w.Name)),
				dropStyle.Render(fmt.Sprintf("%-6d", w.BusDropped)),
				dropStyle.Render(fmt.Sprintf("%-6d", w.WALDropped)),
				humanBytes(w.BytesOnDisk),
				w.Segments,
				dimStyle.Render(formatAgo(time.Since(w.LastUpdate))),
			))
		}
		sections = append(sections, header+"\n"+strings.Join(lines, "\n"))
	}

	// Feeds section.
	feedHeader := amberStyle.Render("  FEED MONITOR")
	if len(feeds) == 0 {
		sections = append(sections, feedHeader+"\n\n  "+dimStyle.Render("No feeds connected"))
	} else {
		colHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(
			fmt.Sprintf("  %-20s  %-12s  %10s  %8s  %s", "SOURCE", "STATUS", "LATENCY", "ERRORS", "LAST UPDATE"),
		)
		var rows []string
		for _, f := range feeds {
			dot := statusDot(f.State)
			stateStr := lipgloss.NewStyle().Foreground(stateColor(f.State)).Render(
				fmt.Sprintf("%-12s", f.State),
			)
			latency := dimStyle.Render(fmt.Sprintf("%8.1fms", f.LatencyMs))
			errStr := dimStyle.Render(fmt.Sprintf("%8d", f.ErrorCount))
			if f.ErrorCount > 0 {
				errStr = redStyle.Render(fmt.Sprintf("%8d", f.ErrorCount))
			}
			ago := time.Since(f.LastUpdate)
			agoStr := dimStyle.Render(formatAgo(ago))
			rows = append(rows, fmt.Sprintf("  %s %-20s  %s  %s  %s  %s",
				dot, f.Name, stateStr, latency, errStr, agoStr))
		}
		sections = append(sections, feedHeader+"\n"+colHeader+"\n"+strings.Join(rows, "\n"))
	}

	// Plugins section.
	pluginHeader := amberStyle.Render("  PLUGIN MONITOR")
	if len(plugins) == 0 {
		sections = append(sections, pluginHeader+"\n\n  "+dimStyle.Render("No plugins loaded"))
	} else {
		colHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(
			fmt.Sprintf("  %-20s  %-12s  %8s  %8s  %s", "PLUGIN", "STATUS", "PID", "ERRORS", "LAST ACTIVITY"),
		)
		var rows []string
		for _, p := range plugins {
			dot := statusDot(p.State)
			stateStr := lipgloss.NewStyle().Foreground(stateColor(p.State)).Render(
				fmt.Sprintf("%-12s", p.State),
			)
			pidStr := dimStyle.Render(fmt.Sprintf("%8d", p.PID))
			errStr := dimStyle.Render(fmt.Sprintf("%8d", p.ErrorCount))
			if p.ErrorCount > 0 {
				errStr = redStyle.Render(fmt.Sprintf("%8d", p.ErrorCount))
			}
			var agoStr string
			if p.LastActivity.IsZero() {
				agoStr = dimStyle.Render("—")
			} else {
				agoStr = dimStyle.Render(formatAgo(time.Since(p.LastActivity)))
			}
			rows = append(rows, fmt.Sprintf("  %s %-20s  %s  %s  %s  %s",
				dot, p.Name, stateStr, pidStr, errStr, agoStr))
		}
		sections = append(sections, pluginHeader+"\n"+colHeader+"\n"+strings.Join(rows, "\n"))
	}

	return strings.Join(sections, "\n\n")
}

func statusDot(state string) string {
	switch state {
	case "connected":
		return greenStyle.Render("●")
	case "reconnecting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("●")
	case "stale":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("●")
	case "error":
		return redStyle.Render("●")
	default:
		return dimStyle.Render("○")
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case "connected":
		return lipgloss.Color("#00FF00")
	case "reconnecting", "stale":
		return lipgloss.Color("#FFFF00")
	case "error":
		return lipgloss.Color("#FF4444")
	default:
		return lipgloss.Color("#666666")
	}
}

// humanBytes formats a byte count as a short human-readable string
// (`1.2K`, `8.4M`, `512.0M`, `1.5G`). Kept tiny + allocation-free
// enough for per-render calls.
func humanBytes(n int64) string {
	const (
		K = 1024
		M = 1024 * 1024
		G = 1024 * 1024 * 1024
	)
	switch {
	case n >= G:
		return fmt.Sprintf("%.1fG", float64(n)/float64(G))
	case n >= M:
		return fmt.Sprintf("%.1fM", float64(n)/float64(M))
	case n >= K:
		return fmt.Sprintf("%.1fK", float64(n)/float64(K))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func formatAgo(d time.Duration) string {
	if d < time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}
