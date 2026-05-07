package plugins

import (
	"testing"
	"time"
)

func TestComputePluginState(t *testing.T) {
	cases := []struct {
		name     string
		running  bool
		since    time.Duration
		staleS   int
		errorS   int
		expected string
	}{
		{"stopped plugin", false, 0, 60, 300, "stopped"},
		{"stopped with long silence", false, 10 * time.Minute, 60, 300, "stopped"},
		{"running, fresh activity", true, time.Second, 60, 300, "connected"},
		{"running, just under stale", true, 59 * time.Second, 60, 300, "connected"},
		{"running, just over stale", true, 61 * time.Second, 60, 300, "stale"},
		{"running, just under error", true, 299 * time.Second, 60, 300, "stale"},
		{"running, just over error", true, 301 * time.Second, 60, 300, "error"},
		{"defaults apply on zero thresholds", true, 90 * time.Second, 0, 0, "stale"},  // default stale=60
		{"defaults apply on zero thresholds, long", true, 400 * time.Second, 0, 0, "error"}, // default error=300
		{"custom thresholds honoured", true, 15 * time.Second, 10, 20, "stale"},
		{"custom thresholds honoured, over error", true, 25 * time.Second, 10, 20, "error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computePluginState(tc.running, tc.since, tc.staleS, tc.errorS)
			if got != tc.expected {
				t.Fatalf("computePluginState(%v, %v, %d, %d) = %q, want %q",
					tc.running, tc.since, tc.staleS, tc.errorS, got, tc.expected)
			}
		})
	}
}
