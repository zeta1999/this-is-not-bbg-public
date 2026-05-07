package views

import (
	"strings"
	"testing"
)

func TestRenderSettings_Empty(t *testing.T) {
	got := RenderSettings(nil, 80, 20)
	if !strings.Contains(got, "No settings to show") {
		t.Fatalf("expected empty hint, got: %s", got)
	}
}

func TestRenderSettings_GroupsBySection(t *testing.T) {
	rows := []SettingRow{
		{Section: "PATHS", Key: "home", Value: "/tmp/nbbg"},
		{Section: "PATHS", Key: "plugins", Value: "/tmp/nbbg/plugins"},
		{Section: "SERVER", Key: "auto_start", Value: "true"},
	}
	got := RenderSettings(rows, 120, 20)
	if !strings.Contains(got, "[PATHS]") || !strings.Contains(got, "[SERVER]") {
		t.Fatalf("missing section headers: %s", got)
	}
	if !strings.Contains(got, "/tmp/nbbg/plugins") || !strings.Contains(got, "auto_start") {
		t.Fatalf("missing rows: %s", got)
	}
}

func TestRenderSettings_ClipsLongValues(t *testing.T) {
	long := strings.Repeat("x", 200)
	rows := []SettingRow{{Section: "PATHS", Key: "home", Value: long}}
	got := RenderSettings(rows, 40, 20)
	if !strings.Contains(got, "…") {
		t.Fatalf("expected ellipsis in narrow output, got: %s", got)
	}
}
