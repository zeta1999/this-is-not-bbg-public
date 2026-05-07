.PHONY: all proto-gen schema-gen build-server build-tui build lint test clean build-cross dist \
       phone-install phone-dev phone-check desktop-dev desktop-build \
       run run-server run-server-oss run-tui run-desktop run-phone smoke icon \
       plugins-full plugins-reduced plugins-oss plugins-clean plugins-list \
       oss-check oss-bootstrap

all: proto-gen build

# --- Protobuf ---

proto-gen:
	buf generate

proto-lint:
	buf lint

# --- JSON Schema for config ---

# Reflects the Go config structs into JSON Schema files under
# libs/proto-ts/schema/ for desktop/phone config-editing UIs to
# validate against. Deterministic output — re-running overwrites in
# place. Source of truth: server/internal/config/config.go.
schema-gen:
	cd server && go run ./cmd/schema-gen -out ../libs/proto-ts/schema

# --- Server ---

build-server:
	cd server && go build -trimpath -o ../bin/notbbg-server ./cmd/notbbg-server

build-collector:
	cd server && go build -trimpath -o ../bin/notbbg-collector ./cmd/notbbg-collector

build-schemacheck:
	cd server && go build -trimpath -o ../bin/schemacheck ./cmd/schemacheck

build-datasoak:
	cd server && go build -trimpath -o ../bin/datasoak ./cmd/datasoak

build-datamigrate:
	cd server && go build -trimpath -o ../bin/datamigrate ./cmd/datamigrate

# --- TUI / CLI ---

build-tui:
	cd tui && go build -trimpath -o ../bin/notbbg ./cmd/notbbg

# --- All builds ---

build: build-server build-collector build-schemacheck build-tui

# --- Distribution (per os/arch with config) ---

TARGETS = darwin-arm64 linux-amd64 linux-arm64 windows-amd64
DIST_DIR = dist

define build-target
	$(eval OS := $(word 1,$(subst -, ,$1)))
	$(eval ARCH := $(word 2,$(subst -, ,$1)))
	$(eval EXT := $(if $(filter windows,$(OS)),.exe,))
	@mkdir -p $(DIST_DIR)/$1/configs
	cd server && GOOS=$(OS) GOARCH=$(ARCH) go build -trimpath -o ../$(DIST_DIR)/$1/notbbg-server$(EXT) ./cmd/notbbg-server
	cd server && GOOS=$(OS) GOARCH=$(ARCH) go build -trimpath -o ../$(DIST_DIR)/$1/notbbg-collector$(EXT) ./cmd/notbbg-collector
	cd tui && GOOS=$(OS) GOARCH=$(ARCH) go build -trimpath -o ../$(DIST_DIR)/$1/notbbg$(EXT) ./cmd/notbbg
	cp server/configs/dev.yaml $(DIST_DIR)/$1/configs/
	cp SKILLS.md $(DIST_DIR)/$1/
endef

dist:
	@rm -rf $(DIST_DIR)
	$(foreach t,$(TARGETS),$(call build-target,$t)$(eval _dummy:=))
	@echo ""
	@echo "Distribution layout:"
	@find $(DIST_DIR) -type f | sort
	@echo ""
	@echo "Each folder is self-contained: executables + configs."

# --- Legacy flat cross-build (kept for compat) ---

build-cross:
	@mkdir -p bin/
	cd server && GOOS=darwin  GOARCH=arm64 go build -trimpath -o ../bin/notbbg-server-darwin-arm64  ./cmd/notbbg-server
	cd server && GOOS=linux   GOARCH=amd64 go build -trimpath -o ../bin/notbbg-server-linux-amd64   ./cmd/notbbg-server
	cd server && GOOS=linux   GOARCH=arm64 go build -trimpath -o ../bin/notbbg-server-linux-arm64   ./cmd/notbbg-server
	cd server && GOOS=windows GOARCH=amd64 go build -trimpath -o ../bin/notbbg-server-windows-amd64.exe ./cmd/notbbg-server
	cd tui && GOOS=darwin  GOARCH=arm64 go build -trimpath -o ../bin/notbbg-darwin-arm64  ./cmd/notbbg
	cd tui && GOOS=linux   GOARCH=amd64 go build -trimpath -o ../bin/notbbg-linux-amd64   ./cmd/notbbg
	cd tui && GOOS=linux   GOARCH=arm64 go build -trimpath -o ../bin/notbbg-linux-arm64   ./cmd/notbbg
	cd tui && GOOS=windows GOARCH=amd64 go build -trimpath -o ../bin/notbbg-windows-amd64.exe ./cmd/notbbg
	@echo "Cross-platform builds complete:"
	@ls -lh bin/notbbg-*

# --- Phone (React Native / Expo) ---

phone-install:
	cd phone && npm install

phone-dev:
	cd phone && npx expo start

phone-check:
	cd phone && npx tsc --noEmit

phone-apk:
	cd phone && npx eas-cli build --platform android --profile preview

# --- Desktop (Electron / React) ---

desktop-install:
	cd desktop && npm install

desktop-dev:
	cd desktop && npm run dev

desktop-build:
	cd desktop && npm run build

desktop-check:
	cd desktop && npx tsc --noEmit

# --- Quality ---

lint:
	cd server && golangci-lint run ./...
	cd tui && golangci-lint run ./...

test:
	cd server && go test -race ./...
	cd tui && go test -race ./...

check: phone-check desktop-check
	@echo "Phone + Desktop TypeScript checks passed."

# Regenerate desktop/assets/icon.{png,icns,ico} from image/logo.svg.
# Portable across macOS / Linux as long as rsvg-convert OR
# ImageMagick is on PATH; iconutil (macOS-only) is required for
# .icns. Run after editing the source SVG.
icon:
	./scripts/build-icon.sh

# --- Run targets — one command per surface, for the operator. ---

# Server in the foreground.
# Source-of-truth config lives at server/configs/dev.yaml (tracked).
# A fresh checkout has no /configs/dev.yaml at the repo root since
# /configs is gitignored — always reach for server/configs.
run-server: build-server
	./bin/notbbg-server -config server/configs/dev.yaml

# OSS-cut server: smaller feed set (binance + free DEX), 7d backfill,
# trimmed cache. Boot-friendly for a fresh clone with no API keys.
# Pair with `make plugins-oss` (or `make oss-bootstrap` for both).
run-server-oss: build-server
	./bin/notbbg-server -config server/configs/dev-oss.yaml

# TUI (auto-pairs against the local server's TUI token).
run-tui: build-tui
	./bin/notbbg

# Desktop Electron — expects the server to be running.
run-desktop:
	cd desktop && npm run electron

# Phone via Expo dev server — expects the server to be running.
run-phone:
	cd phone && npx expo start

run: run-server

# Health probe — server must be up. macOS system curl can fail
# the TLS 1.3 handshake against our self-signed cert; fall back to
# /opt/homebrew/opt/curl/bin/curl when available, then nc as a
# last-ditch port-open check.
smoke:
	@HC=$$(/opt/homebrew/opt/curl/bin/curl -ks https://localhost:9474/api/v1/health 2>/dev/null \
	    || curl -ks --tlsv1.3 https://localhost:9474/api/v1/health 2>/dev/null \
	    || true); \
	if [ -n "$$HC" ]; then echo "$$HC"; \
	elif nc -z localhost 9474 2>/dev/null; then echo "port 9474 open (TLS handshake skipped)"; \
	else echo "server not up?"; fi
	@echo ""

# --- Plugin profiles ---
# Two named selections of example plugins for fast OSS / demo
# switching. Profiles live in configs/plugin-profiles/<name>.txt.
# `--replace` drops anything outside the profile so the deployed
# directory matches the profile exactly.

plugins-full:
	./scripts/deploy-plugins.sh --profile full --replace

plugins-reduced:
	./scripts/deploy-plugins.sh --profile reduced --replace

# OSS profile: SDK demos + self-contained example plugins only
# (hello-world, hello-rs, formula-demo, formulettes-runner, monitor,
# ohlc-png). Pair with `make run-server-oss` for a fresh-clone
# friendly setup.
plugins-oss:
	./scripts/deploy-plugins.sh --profile oss --replace

plugins-clean:
	./scripts/deploy-plugins.sh --clean

plugins-list:
	./scripts/deploy-plugins.sh --list

# Verify the OSS / reduced cut still builds + tests + typechecks
# end-to-end, and that no plugin-specific identifiers have leaked
# into core paths. Run before publishing an OSS release.
oss-check:
	./scripts/oss-check.sh

# One-shot bootstrap for someone trying the OSS cut: deploy the OSS
# plugin profile and start the server with the matching dev-oss.yaml.
# Equivalent to `make plugins-oss && make run-server-oss` but easier
# to point new users at.
oss-bootstrap: plugins-oss run-server-oss

# --- Clean ---

clean:
	rm -rf bin/ dist/
