#!/usr/bin/env bash
set -euo pipefail

echo "=== Proto lint ==="
buf lint

echo "=== Proto generate ==="
buf generate

echo "=== Test server ==="
(cd server && go test -race ./...)

echo "=== Fuzz hot paths (quick soak) ==="
# Short soaks — surface any new-crash regressions without blowing the
# CI budget. Longer soaks happen out of band via -fuzztime=... .
(cd server \
  && go test -run=^$ -fuzz=FuzzReader      -fuzztime=10s ./internal/datalake \
  && go test -run=^$ -fuzz=FuzzTopicRouter -fuzztime=10s ./internal/bus \
  && go test -run=^$ -fuzz=FuzzStorePutGet -fuzztime=10s ./internal/cache)

echo "=== Test TUI ==="
(cd tui && go test -race ./...)

echo "=== Lint server ==="
(cd server && golangci-lint run ./...)

echo "=== Lint TUI ==="
(cd tui && golangci-lint run ./...)

echo "=== All checks passed ==="
