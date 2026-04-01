#!/usr/bin/env bash
set -euo pipefail

echo "=== Proto lint ==="
buf lint

echo "=== Proto generate ==="
buf generate

echo "=== Test server ==="
(cd server && go test -race ./...)

echo "=== Test TUI ==="
(cd tui && go test -race ./...)

echo "=== Lint server ==="
(cd server && golangci-lint run ./...)

echo "=== Lint TUI ==="
(cd tui && golangci-lint run ./...)

echo "=== All checks passed ==="
