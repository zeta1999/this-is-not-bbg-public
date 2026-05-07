package views

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMonitor_BusAndWAL(t *testing.T) {
	busStats := BusStatsEntry{
		Subscribers: 5,
		Topics:      12,
		Dropped:     7,
		LastUpdate:  time.Now().Add(-5 * time.Second),
	}
	wals := []WALStatsEntry{
		{Name: "cache", Enqueued: 1000, BusDropped: 0, WALDropped: 0, BytesOnDisk: 1024 * 1024, Segments: 2, LastUpdate: time.Now()},
		{Name: "datalake", Written: 2000, BusDropped: 3, WALDropped: 0, BytesOnDisk: 60 * 1024 * 1024, Segments: 4, LastUpdate: time.Now()},
	}
	out := RenderMonitorFull(nil, nil, busStats, wals, 120, 30)
	for _, want := range []string{"BACKPRESSURE", "subs=5", "topics=12", "dropped=7", "wal.cache", "wal.datalake", "1.0M", "60.0M"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderMonitor_NoBackpressureWhenEmpty(t *testing.T) {
	out := RenderMonitorFull(nil, nil, BusStatsEntry{}, nil, 120, 30)
	if strings.Contains(out, "BACKPRESSURE") {
		t.Errorf("should not render BACKPRESSURE when no stats seen: %s", out)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0K"},
		{1024 * 1024, "1.0M"},
		{5 * 1024 * 1024 * 1024, "5.0G"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d)=%q, want %q", c.n, got, c.want)
		}
	}
}
