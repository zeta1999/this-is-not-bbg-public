package transport

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// allowedArtifactExts is the whitelist of file extensions the
// artifact endpoint will serve. Image cells in plugin grids and
// NOTBBG:/path agent artifacts both flow through here.
var allowedArtifactExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".pdf":  "application/pdf",
}

// artifactRoots returns the directories under which the artifact
// endpoint is permitted to serve files.
//
// If $NOTBBG_ARTIFACT_DIRS (colon-separated) is set, it is the
// EXCLUSIVE allowlist — operators get to lock down to specific
// plugin-output directories.
//
// Otherwise, the defaults cover the conventions plugins follow:
// $TMPDIR (macOS /var/folders) and /tmp (Linux). Symlinks in the
// roots are resolved up front so the allowlist check works on the
// same canonical paths EvalSymlinks produces for served files.
func artifactRoots() []string {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	if extra := os.Getenv("NOTBBG_ARTIFACT_DIRS"); extra != "" {
		var roots []string
		for _, p := range strings.Split(extra, ":") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			roots = append(roots, resolve(p))
		}
		return roots
	}
	var roots []string
	if t := os.TempDir(); t != "" {
		roots = append(roots, resolve(t))
	}
	roots = append(roots, "/tmp")
	return roots
}

// maxArtifactBytes is the cap on artifact body size. Images sent
// through plugin cells should be small; the cap keeps a misbehaving
// plugin from streaming gigabytes through the gateway. Operators
// can raise it via $NOTBBG_ARTIFACT_MAX_MB.
func maxArtifactBytes() int64 {
	const def int64 = 10 * 1024 * 1024
	if s := os.Getenv("NOTBBG_ARTIFACT_MAX_MB"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return n * 1024 * 1024
		}
	}
	return def
}

// errArtifactDenied is returned when path resolution fails the
// allowlist check or the extension whitelist. The handler converts
// it into a 403; tests use errors.Is to distinguish from 404.
var errArtifactDenied = errors.New("artifact path not permitted")

// resolveArtifactPath turns a user-supplied src (with or without the
// NOTBBG: prefix) into an absolute, symlink-resolved file path that
// is inside one of the allowlisted roots and has an allowed extension.
// It deliberately does NOT stat the file — the caller does that so
// the missing-file vs denied-path distinction can map to 404 vs 403.
func resolveArtifactPath(src string) (string, error) {
	src = strings.TrimPrefix(src, "NOTBBG:")
	src = strings.TrimSpace(src)
	if src == "" {
		return "", errArtifactDenied
	}
	if !filepath.IsAbs(src) {
		return "", errArtifactDenied
	}
	ext := strings.ToLower(filepath.Ext(src))
	if _, ok := allowedArtifactExts[ext]; !ok {
		return "", errArtifactDenied
	}
	clean := filepath.Clean(src)
	// Resolve symlinks. If the file itself exists, EvalSymlinks
	// gives the canonical path. If it doesn't, fall back to
	// resolving the parent directory so the allowlist check still
	// runs against the canonical root (matters on macOS where
	// /var → /private/var). The os.Stat in the handler then
	// surfaces ErrNotExist for the 404 path.
	resolved := clean
	if r, err := filepath.EvalSymlinks(clean); err == nil {
		resolved = r
	} else if rd, err := filepath.EvalSymlinks(filepath.Dir(clean)); err == nil {
		resolved = filepath.Join(rd, filepath.Base(clean))
	}
	roots := artifactRoots()
	for _, root := range roots {
		// Compare on cleaned absolute paths with a trailing
		// separator to prevent /tmp-evil matching a /tmp root.
		rootClean := filepath.Clean(root)
		if rootClean == resolved {
			return resolved, nil
		}
		if strings.HasPrefix(resolved, rootClean+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", errArtifactDenied
}

// handleArtifact serves a plugin-generated artifact (image, pdf, ...)
// after path sanitization. Used by desktop ImageCellView, phone
// <Image source>, and TUI's `o` shortcut. The endpoint is auth-gated
// like every other data endpoint.
func (g *HTTPGateway) handleArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, `{"error":"GET required"}`, http.StatusMethodNotAllowed)
		return
	}
	src := r.URL.Query().Get("path")
	if src == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}
	path, err := resolveArtifactPath(src)
	if err != nil {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"stat failed"}`, http.StatusInternalServerError)
		return
	}
	if st.IsDir() {
		http.Error(w, `{"error":"is a directory"}`, http.StatusForbidden)
		return
	}
	if max := maxArtifactBytes(); st.Size() > max {
		http.Error(w, `{"error":"artifact too large"}`, http.StatusRequestEntityTooLarge)
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	ct := allowedArtifactExts[ext]
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == "HEAD" {
		w.WriteHeader(http.StatusOK)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, `{"error":"open failed"}`, http.StatusInternalServerError)
		return
	}
	defer f.Close()
	_, _ = io.CopyN(w, f, st.Size())
}
