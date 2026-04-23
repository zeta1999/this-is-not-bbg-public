package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("root", "", "datalake root (required)")
	strict := flag.Bool("strict", false, "exit 1 if any mismatch")
	sample := flag.Int("sample", 0, "records per file to sample (0=all)")
	flag.Parse()

	if *root == "" {
		fmt.Fprintln(os.Stderr, "usage: schemacheck --root <datalake-path> [--strict] [--sample N]")
		os.Exit(2)
	}

	rep := NewReport()

	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			log.Printf("open %s: %v", path, err)
			return nil
		}
		defer f.Close()
		rep.Files++
		if err := Scan(rep, f, path, *sample); err != nil {
			log.Printf("scan %s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	FormatReport(os.Stdout, rep)

	if *strict && rep.AnyMismatch() {
		os.Exit(1)
	}
}
