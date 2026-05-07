package transport

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveArtifactPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOTBBG_ARTIFACT_DIRS", dir)

	// Tests compare against the canonical (symlink-resolved) form
	// because resolveArtifactPath returns the canonical path.
	canonDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		canonDir = dir
	}

	good := filepath.Join(dir, "ok.png")
	if err := os.WriteFile(good, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"happy path", good, true},
		{"NOTBBG prefix", "NOTBBG:" + good, true},
		{"trailing whitespace", "  " + good + "  ", true},
		{"relative path rejected", "ok.png", false},
		{"empty rejected", "", false},
		{"bad extension rejected", filepath.Join(dir, "x.exe"), false},
		{"path traversal rejected", filepath.Join(dir, "..", "etc-passwd.png"), false},
		{"outside roots rejected", "/etc/hosts.png", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := resolveArtifactPath(c.in)
			if c.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.HasPrefix(out, canonDir) {
					t.Fatalf("resolved path %q escapes root %q", out, canonDir)
				}
			} else {
				if !errors.Is(err, errArtifactDenied) {
					t.Fatalf("expected errArtifactDenied, got %v", err)
				}
			}
		})
	}
}

func TestResolveArtifactPathSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	t.Setenv("NOTBBG_ARTIFACT_DIRS", dir)

	// Create a real file outside the allowlist, then a symlink
	// inside the allowlist that points at it. The resolver must
	// re-check the allowlist on the symlink target.
	target := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}
	if _, err := resolveArtifactPath(link); !errors.Is(err, errArtifactDenied) {
		t.Fatalf("symlink escape was permitted: err=%v", err)
	}
}

func TestHandleArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOTBBG_ARTIFACT_DIRS", dir)
	body := []byte("\x89PNG\r\n\x1a\nfake")
	good := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(good, body, 0o644); err != nil {
		t.Fatal(err)
	}

	g := &HTTPGateway{}

	t.Run("serves PNG with correct Content-Type", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/artifact?path="+good, nil)
		w := httptest.NewRecorder()
		g.handleArtifact(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != "image/png" {
			t.Errorf("Content-Type=%q want image/png", got)
		}
		got, _ := io.ReadAll(w.Body)
		if string(got) != string(body) {
			t.Errorf("body mismatch")
		}
	})

	t.Run("rejects missing path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/artifact", nil)
		w := httptest.NewRecorder()
		g.handleArtifact(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", w.Code)
		}
	})

	t.Run("rejects path outside roots", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/artifact?path=/etc/hosts.png", nil)
		w := httptest.NewRecorder()
		g.handleArtifact(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d", w.Code)
		}
	})

	t.Run("404 for missing file under permitted root", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/artifact?path="+filepath.Join(dir, "nope.png"), nil)
		w := httptest.NewRecorder()
		g.handleArtifact(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
	})

	t.Run("rejects oversize", func(t *testing.T) {
		t.Setenv("NOTBBG_ARTIFACT_MAX_MB", "0") // forces default
		// Write a file larger than 10MB cap.
		big := filepath.Join(dir, "big.png")
		f, err := os.Create(big)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(11 * 1024 * 1024); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		req := httptest.NewRequest("GET", "/api/v1/artifact?path="+big, nil)
		w := httptest.NewRecorder()
		g.handleArtifact(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("HEAD does not stream body", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/api/v1/artifact?path="+good, nil)
		w := httptest.NewRecorder()
		g.handleArtifact(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("HEAD wrote body: %d bytes", w.Body.Len())
		}
	})
}
