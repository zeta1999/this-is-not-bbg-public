// datamigrate walks an old-layout datalake tree and rewrites every
// JSONL file into the new dim-ordered layout from
// server/internal/dimpath.
//
// Old layout:
//
//	<root>/type=ohlc/exchange=binance/instrument=BTCUSDT/year=2026/month=04/day=22/data.jsonl
//
// New layout:
//
//	<root>/dims=exchange=binance;instrument=BTCUSDT;type=ohlc/year=2026/month=04/day=22/data.jsonl
//
// Dry-run is the default — nothing is written until --apply is set.
// The tool is idempotent: rewriting an already-rewritten tree is a
// no-op because the new paths are canonical.
//
// See DATA-PLAN.md §5.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/notbbg/notbbg/server/internal/dimpath"
)

func main() {
	root := flag.String("root", "", "datalake root (required)")
	apply := flag.Bool("apply", false, "actually move files; default is dry-run")
	keep := flag.Bool("keep-old", false, "after --apply, leave the old layout in place (default: delete empty dirs)")
	flag.Parse()

	if *root == "" {
		fmt.Fprintln(os.Stderr, "usage: datamigrate --root <datalake-path> [--apply] [--keep-old]")
		os.Exit(2)
	}

	plan, err := buildPlan(*root)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}

	fmt.Printf("files to migrate: %d\n", len(plan))
	if len(plan) == 0 {
		fmt.Println("nothing to do")
		return
	}

	// Print the first few moves so the operator can sanity-check.
	show := len(plan)
	if show > 10 {
		show = 10
	}
	for _, m := range plan[:show] {
		fmt.Printf("  %s\n  →  %s\n", rel(*root, m.Src), rel(*root, m.Dst))
	}
	if len(plan) > show {
		fmt.Printf("  … %d more\n", len(plan)-show)
	}

	if !*apply {
		fmt.Println("\ndry-run — re-run with --apply to move files")
		return
	}

	// Execute.
	var done, skipped, failed int
	for _, m := range plan {
		res := apply1(m)
		switch res {
		case resultDone:
			done++
		case resultSkipped:
			skipped++
		case resultFailed:
			failed++
		}
	}
	fmt.Printf("\ndone=%d skipped=%d failed=%d\n", done, skipped, failed)

	if !*keep {
		pruneEmptyDirs(*root)
	}

	if failed > 0 {
		os.Exit(1)
	}
}

type move struct {
	Src string
	Dst string
}

// buildPlan walks root and produces the list of rewrites needed.
// Already-canonical paths are skipped.
func buildPlan(root string) ([]move, error) {
	var plan []move
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		dst, ok := rewritePath(root, path)
		if !ok || dst == path {
			return nil
		}
		plan = append(plan, move{Src: path, Dst: dst})
		return nil
	})
	return plan, err
}

// rewritePath maps an old path to its canonical dim-ordered
// equivalent. Returns ok=false if the path doesn't look like the
// old layout at all (already migrated, or junk).
func rewritePath(root, path string) (string, bool) {
	relp, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	parts := strings.Split(relp, string(filepath.Separator))

	dims := map[string]string{}
	var tail []string
	consumedLegacy := false
	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "type="):
			dims["type"] = strings.TrimPrefix(p, "type=")
			consumedLegacy = true
		case strings.HasPrefix(p, "exchange="):
			dims["exchange"] = strings.TrimPrefix(p, "exchange=")
			consumedLegacy = true
		case strings.HasPrefix(p, "instrument="):
			dims["instrument"] = strings.TrimPrefix(p, "instrument=")
			consumedLegacy = true
		case strings.HasPrefix(p, "source="):
			dims["source"] = strings.TrimPrefix(p, "source=")
			consumedLegacy = true
		default:
			// Everything else (year=/month=/day=/hour=/data.jsonl) is
			// carried through verbatim.
			tail = append(tail, p)
		}
	}
	if !consumedLegacy {
		return "", false
	}
	if len(dims) == 0 {
		return "", false
	}
	segment := dimpath.Build(dims)
	newRel := filepath.Join(append([]string{segment}, tail...)...)
	return filepath.Join(root, newRel), true
}

type result int

const (
	resultDone result = iota
	resultSkipped
	resultFailed
)

func apply1(m move) result {
	if _, err := os.Stat(m.Dst); err == nil {
		// Destination already exists — append rather than overwrite
		// so migrations never lose data.
		if err := appendFile(m.Src, m.Dst); err != nil {
			log.Printf("append %s → %s: %v", m.Src, m.Dst, err)
			return resultFailed
		}
		if err := os.Remove(m.Src); err != nil {
			log.Printf("remove src %s: %v", m.Src, err)
			return resultFailed
		}
		return resultDone
	}

	if err := os.MkdirAll(filepath.Dir(m.Dst), 0o755); err != nil {
		log.Printf("mkdir %s: %v", filepath.Dir(m.Dst), err)
		return resultFailed
	}
	if err := os.Rename(m.Src, m.Dst); err == nil {
		return resultDone
	} else {
		// Cross-device rename fails on some filesystems; fall back to copy + delete.
		if err := copyFile(m.Src, m.Dst); err != nil {
			log.Printf("copy %s → %s: %v", m.Src, m.Dst, err)
			return resultFailed
		}
		if err := os.Remove(m.Src); err != nil {
			log.Printf("remove src %s: %v", m.Src, err)
			return resultFailed
		}
		return resultDone
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func appendFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// pruneEmptyDirs removes empty legacy partition dirs (type=*/…) so
// the tree is clean after a migration. Only drops dirs that are
// actually empty; safe.
func pruneEmptyDirs(root string) {
	// Walk bottom-up.
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	// Longest paths first so leaves are removed before their parents.
	for i := len(dirs) - 1; i >= 0; i-- {
		p := dirs[i]
		if p == root {
			continue
		}
		entries, err := os.ReadDir(p)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(p)
		}
	}
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}
