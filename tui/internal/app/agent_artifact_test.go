package app

import "testing"

func TestLatestAgentArtifact_EmptyOrMissing(t *testing.T) {
	if got := latestAgentArtifact(nil); got != "" {
		t.Fatalf("empty lines: got %q", got)
	}
	if got := latestAgentArtifact([]string{"hello", "world"}); got != "" {
		t.Fatalf("no tag: got %q", got)
	}
}

func TestLatestAgentArtifact_ReturnsMostRecent(t *testing.T) {
	lines := []string{
		"plot generated NOTBBG:/tmp/a.png",
		"just text",
		"final NOTBBG:/tmp/latest.png and more.",
	}
	got := latestAgentArtifact(lines)
	if got != "/tmp/latest.png" {
		t.Fatalf("got %q, want /tmp/latest.png", got)
	}
}

func TestLatestAgentArtifact_StripsTrailingPunct(t *testing.T) {
	lines := []string{"see NOTBBG:/var/tmp/x.png."}
	if got := latestAgentArtifact(lines); got != "/var/tmp/x.png" {
		t.Fatalf("got %q, want /var/tmp/x.png", got)
	}
}

func TestLatestAgentArtifact_IgnoresBarePrefix(t *testing.T) {
	// NOTBBG: with no path should be ignored — continue searching
	// earlier lines for a real artifact.
	lines := []string{
		"old NOTBBG:/tmp/old.png",
		"NOTBBG:",
	}
	if got := latestAgentArtifact(lines); got != "/tmp/old.png" {
		t.Fatalf("got %q, want /tmp/old.png", got)
	}
}

func TestOpenArtifact_ShimmedExec(t *testing.T) {
	var cmd string
	var args []string
	orig := osExec
	osExec = func(c string, a ...string) error {
		cmd = c
		args = a
		return nil
	}
	defer func() { osExec = orig }()

	if err := openArtifact("/tmp/x.png"); err != nil {
		t.Fatal(err)
	}
	if cmd != "xdg-open" && cmd != "open" {
		t.Fatalf("cmd=%q, want open or xdg-open", cmd)
	}
	if len(args) != 1 || args[0] != "/tmp/x.png" {
		t.Fatalf("args=%v, want [/tmp/x.png]", args)
	}
}
