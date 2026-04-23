#!/bin/bash
# deploy-plugins.sh — build and install plugins to ~/.config/notbbg/plugins/
#
# Usage:
#   ./scripts/deploy-plugins.sh              # deploy all plugins
#   ./scripts/deploy-plugins.sh pricer       # deploy only pricer
#   ./scripts/deploy-plugins.sh pricer hello-world  # deploy specific plugins
#   ./scripts/deploy-plugins.sh --list       # list available plugins
#   ./scripts/deploy-plugins.sh --clean      # remove all deployed plugins
#
# Environment:
#   PLUGIN_DIR  override install directory (default: ~/.config/notbbg/plugins)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLUGIN_SRC="$REPO_ROOT/examples/plugins"
PLUGIN_DIR="${PLUGIN_DIR:-$HOME/.config/notbbg/plugins}"

# Discover available plugins (any dir under examples/plugins/ with a manifest.yaml).
available_plugins() {
    for d in "$PLUGIN_SRC"/*/; do
        [ -f "$d/manifest.yaml" ] && basename "$d"
    done
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

if [ $# -gt 0 ]; then
    PLUGINS="$@"
else
    PLUGINS=$(available_plugins)
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

for p in $PLUGINS; do
    echo "=== $p ==="
    src="$PLUGIN_SRC/$p"
    dst="$PLUGIN_DIR/$p"

    # Build.
    echo "  building..."
    if (cd "$src" && go build -o "$p" . 2>&1); then
        mkdir -p "$dst"
        cp "$src/$p" "$dst/$p"
        cp "$src/manifest.yaml" "$dst/manifest.yaml"
        rm -f "$src/$p"  # clean build artifact from source
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
