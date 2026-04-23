.PHONY: all proto-gen build-server build-tui build lint test clean build-cross dist \
       phone-install phone-dev phone-check desktop-dev desktop-build

all: proto-gen build

# --- Protobuf ---

proto-gen:
	buf generate

proto-lint:
	buf lint

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

# --- Clean ---

clean:
	rm -rf bin/ dist/
