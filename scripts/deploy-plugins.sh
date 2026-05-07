#!/bin/bash
# deploy-plugins.sh — build and install plugins to ~/.config/notbbg/plugins/
#
# Usage:
#   ./scripts/deploy-plugins.sh                       # deploy every plugin in examples/plugins
#   ./scripts/deploy-plugins.sh pricer                # deploy only pricer
#   ./scripts/deploy-plugins.sh pricer hello-world    # deploy specific plugins
#   ./scripts/deploy-plugins.sh --list                # list available plugins
#   ./scripts/deploy-plugins.sh --clean               # remove all deployed plugins
#   ./scripts/deploy-plugins.sh --profile reduced     # deploy a named profile
#                                                    #   (file: scripts/plugin-profiles/<name>.txt)
#   ./scripts/deploy-plugins.sh --profile reduced --replace
#                                                    # also remove anything outside the profile
#   ./scripts/deploy-plugins.sh --profile oss --replace
#                                                    # OSS-release plugin set (demos + self-contained
#                                                    # examples; pair with server/configs/dev-oss.yaml)
#
# Environment:
#   PLUGIN_DIR  override install directory (default: ~/.config/notbbg/plugins)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLUGIN_SRC="$REPO_ROOT/examples/plugins"
PROFILE_DIR="$REPO_ROOT/scripts/plugin-profiles"
PLUGIN_DIR="${PLUGIN_DIR:-$HOME/.config/notbbg/plugins}"

# Discover available plugins (any dir under examples/plugins/ with a manifest.yaml).
available_plugins() {
    for d in "$PLUGIN_SRC"/*/; do
        [ -f "$d/manifest.yaml" ] && basename "$d"
    done
}

# Read a profile file (configs/plugin-profiles/<name>.txt). Strips
# `#` comments and blank lines so the file is human-editable.
profile_plugins() {
    local name="$1"
    local file="$PROFILE_DIR/$name.txt"
    if [ ! -f "$file" ]; then
        echo "ERROR: profile '$name' not found at $file" >&2
        echo "Available profiles: $(ls "$PROFILE_DIR" 2>/dev/null | sed 's/\.txt$//' | tr '\n' ' ')" >&2
        exit 1
    fi
    sed -e 's/#.*$//' -e '/^[[:space:]]*$/d' "$file"
}

# --- Flags -------------------------------------------------------------------

if [ "$1" = "--list" ]; then
    echo "Available plugins:"
    for p in $(available_plugins); do
        deployed=""
        [ -f "$PLUGIN_DIR/$p/manifest.yaml" ] && deployed=" [deployed]"
        echo "  $p$deployed"
    done
    exit 0
fi

if [ "$1" = "--clean" ]; then
    echo "Removing all deployed plugins from $PLUGIN_DIR"
    for p in $(available_plugins); do
        if [ -d "$PLUGIN_DIR/$p" ]; then
            rm -rf "$PLUGIN_DIR/$p"
            echo "  removed $p"
        fi
    done
    echo "Done."
    exit 0
fi

# --- Select plugins to deploy ------------------------------------------------

PROFILE=""
REPLACE=0
ARGS=()
while [ $# -gt 0 ]; do
    case "$1" in
        --profile)
            PROFILE="$2"
            shift 2
            ;;
        --profile=*)
            PROFILE="${1#--profile=}"
            shift
            ;;
        --replace)
            REPLACE=1
            shift
            ;;
        *)
            ARGS+=("$1")
            shift
            ;;
    esac
done

if [ -n "$PROFILE" ]; then
    PLUGINS=$(profile_plugins "$PROFILE")
    echo "Profile '$PROFILE' selects: $(echo $PLUGINS | tr '\n' ' ')"
elif [ ${#ARGS[@]} -gt 0 ]; then
    PLUGINS="${ARGS[@]}"
else
    PLUGINS=$(available_plugins)
fi

# --replace: drop any deployed plugin that's NOT in the chosen set.
# Lets `--profile reduced --replace` match the profile exactly without
# leaving stragglers around from a previous full deploy.
if [ $REPLACE -eq 1 ]; then
    KEEP=" $(echo $PLUGINS | tr '\n' ' ') "
    for p in $(available_plugins); do
        if [ -d "$PLUGIN_DIR/$p" ] && [[ "$KEEP" != *" $p "* ]]; then
            rm -rf "$PLUGIN_DIR/$p"
            echo "  removed (not in profile): $p"
        fi
    done
fi

# Validate selections.
for p in $PLUGINS; do
    if [ ! -d "$PLUGIN_SRC/$p" ]; then
        echo "ERROR: plugin '$p' not found in $PLUGIN_SRC"
        echo "Available: $(available_plugins | tr '\n' ' ')"
        exit 1
    fi
    if [ ! -f "$PLUGIN_SRC/$p/manifest.yaml" ]; then
        echo "ERROR: plugin '$p' has no manifest.yaml"
        exit 1
    fi
done

# --- Build and deploy --------------------------------------------------------

mkdir -p "$PLUGIN_DIR"

BUILT=0
FAILED=0

# has_non_test_go <dir>: returns 0 iff <dir> has at least one .go
# file that is NOT a *_test.go. The deploy script uses this to
# distinguish Go source plugins from Python/Rust plugins that just
# happen to ship a Go test harness alongside their real source.
has_non_test_go() {
    local dir="$1"
    local f
    for f in "$dir"/*.go; do
        [ -e "$f" ] || continue
        case "$f" in
            *_test.go) ;;
            *) return 0 ;;
        esac
    done
    return 1
}

# build_plugin <name> <src> <dst>
# Picks the right toolchain based on what's in the source dir:
#   - Makefile            → make all; the Makefile knows how to build
#                           multi-binary plugins (e.g. formulettes-runner
#                           ships a Go runner + a C++ bs-cli companion)
#   - Cargo.toml          → cargo build --release, copy target/release/<name>
#   - main.py             → Python plugin; copy *.py + manifest as-is
#                           (operator owns the per-plugin venv per the
#                            ohlc-png README — we don't auto-create it)
#   - non-test .go files  → go build -o <name> .
# Returns 0 on success.
build_plugin() {
    local name="$1"
    local src="$2"
    local dst="$3"

    if [ -f "$src/Makefile" ]; then
        echo "  building (make)..."
        if ! (cd "$src" && make all 2>&1); then
            return 1
        fi
        mkdir -p "$dst"
        cp "$src/manifest.yaml" "$dst/manifest.yaml"
        # Copy every executable file produced in the source root.
        # The Makefile may build the named plugin binary plus
        # companion CLIs (e.g. bs-cli for formulettes-runner) — both
        # need to land alongside the manifest so the plugin's argv
        # spawns find them.
        local f copied=0
        for f in "$src"/*; do
            [ -f "$f" ] || continue
            [ -x "$f" ] || continue
            case "$(basename "$f")" in
                Makefile|*.sh|*.py|*.go|*.cpp|*.h) continue ;;
            esac
            cp "$f" "$dst/"
            copied=$((copied + 1))
        done
        if [ $copied -eq 0 ]; then
            echo "  ERROR: make completed but no executables found in $src"
            return 1
        fi
        return 0
    fi

    if [ -f "$src/Cargo.toml" ]; then
        echo "  building (cargo)..."
        if ! (cd "$src" && cargo build --release --quiet 2>&1); then
            return 1
        fi
        local rust_bin="$src/target/release/$name"
        if [ ! -x "$rust_bin" ]; then
            echo "  ERROR: cargo built but $rust_bin missing"
            return 1
        fi
        mkdir -p "$dst"
        cp "$rust_bin" "$dst/$name"
        cp "$src/manifest.yaml" "$dst/manifest.yaml"
        return 0
    fi

    if [ -f "$src/main.py" ]; then
        # Python plugins: no compile step. Manifest's `command` is
        # the venv interpreter (e.g. `.venv/bin/python main.py`),
        # which the server invokes directly. We just stage source
        # files; operator creates the venv per the plugin's README.
        echo "  staging (python)..."
        mkdir -p "$dst"
        cp "$src/manifest.yaml" "$dst/manifest.yaml"
        local f
        for f in "$src"/*.py; do
            [ -e "$f" ] || continue
            cp "$f" "$dst/"
        done
        # Carry requirements.txt + setup hint files so the operator
        # can `pip install -r requirements.txt` after `python -m
        # venv .venv` lands in the deployed dir.
        for f in requirements.txt pyproject.toml; do
            [ -f "$src/$f" ] && cp "$src/$f" "$dst/"
        done
        if [ ! -d "$dst/.venv" ]; then
            echo "  NOTE: $name needs a venv at $dst/.venv (see $src/README.md)"
        fi
        return 0
    fi

    if has_non_test_go "$src"; then
        echo "  building (go)..."
        if ! (cd "$src" && go build -o "$name" . 2>&1); then
            return 1
        fi
        mkdir -p "$dst"
        cp "$src/$name" "$dst/$name"
        cp "$src/manifest.yaml" "$dst/manifest.yaml"
        rm -f "$src/$name" # clean build artifact from source
        return 0
    fi

    echo "  ERROR: $src has no recognised toolchain (Cargo.toml / main.py / *.go)"
    return 1
}

for p in $PLUGINS; do
    echo "=== $p ==="
    src="$PLUGIN_SRC/$p"
    dst="$PLUGIN_DIR/$p"

    if build_plugin "$p" "$src" "$dst"; then
        echo "  deployed → $dst"
        BUILT=$((BUILT + 1))
    else
        echo "  FAILED to build $p"
        FAILED=$((FAILED + 1))
    fi
    echo ""
done

# --- Summary -----------------------------------------------------------------

echo "=== Done ==="
echo "  Deployed: $BUILT"
[ $FAILED -gt 0 ] && echo "  Failed:   $FAILED"
echo "  Plugin dir: $PLUGIN_DIR"
echo ""
echo "Restart the server to pick up changes."
