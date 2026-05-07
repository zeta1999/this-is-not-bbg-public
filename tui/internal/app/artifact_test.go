package app

import (
	"errors"
	"runtime"
	"testing"
)

func TestLatestAgentArtifact(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"no tag", []string{"hello world", "foo bar"}, ""},
		{"single tag", []string{"NOTBBG:/tmp/a.png"}, "/tmp/a.png"},
		{
			"strips trailing punctuation",
			[]string{"see NOTBBG:/tmp/b.png."},
			"/tmp/b.png",
		},
		{
			"returns the LAST tag when multiple appear",
			[]string{"NOTBBG:/tmp/old.png", "later: NOTBBG:/tmp/new.png"},
			"/tmp/new.png",
		},
		{"ignores non-prefixed tokens", []string{"BBG:/tmp/x.png"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := latestAgentArtifact(c.in)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestOpenArtifact_UsesPlatformOpener(t *testing.T) {
	// Swap osExec to capture the call. Restore after.
	var gotCmd string
	var gotArgs []string
	prev := osExec
	osExec = func(cmd string, args ...string) error {
		gotCmd = cmd
		gotArgs = args
		return nil
	}
	defer func() { osExec = prev }()

	if err := openArtifact("/tmp/img.png"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		wantCmd = "open"
	}
	if gotCmd != wantCmd {
		t.Errorf("opener cmd: got %q want %q", gotCmd, wantCmd)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "/tmp/img.png" {
		t.Errorf("opener args: got %v want [/tmp/img.png]", gotArgs)
	}
}

func TestOpenArtifact_PropagatesError(t *testing.T) {
	prev := osExec
	osExec = func(cmd string, args ...string) error { return errors.New("boom") }
	defer func() { osExec = prev }()
	if err := openArtifact("/tmp/x.png"); err == nil {
		t.Fatal("expected error to propagate")
	}
}
