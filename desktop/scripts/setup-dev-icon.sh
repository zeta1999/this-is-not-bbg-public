#!/usr/bin/env bash
# Override the bundled Electron.app's icon + display name so the
# macOS dock, Cmd+Tab switcher, and Force Quit Applications dialog
# show the notbbg branding during `npm run electron` (dev mode)
# instead of the default Electron logo.
#
# Why this is necessary: macOS reads CFBundleIconFile and
# CFBundleDisplayName from the running .app's Info.plist, NOT from
# Electron's runtime app.dock.setIcon() / app.setName() calls.
# In dev mode the running .app is node_modules/electron/dist/
# Electron.app — pre-built, generic. Replacing its icon resource
# is the only way to retag the dock identity for dev.
#
# Idempotent. Safe to run twice. No-op on non-darwin / when the
# Electron.app bundle isn't where we expect.
#
# Wired as a "postinstall" script in desktop/package.json so any
# fresh `npm install` re-applies the override (npm wipes
# node_modules contents on reinstall).
set -euo pipefail

[[ "$(uname)" == "Darwin" ]] || { echo "[setup-dev-icon] non-darwin host, skipping"; exit 0; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ICON_SRC="$ROOT/assets/icon.icns"
APP="$ROOT/node_modules/electron/dist/Electron.app"
RES="$APP/Contents/Resources"
PLIST="$APP/Contents/Info.plist"

if [[ ! -d "$APP" ]]; then
  echo "[setup-dev-icon] $APP not found — was npm install run?"
  exit 0
fi
if [[ ! -f "$ICON_SRC" ]]; then
  echo "[setup-dev-icon] $ICON_SRC missing — run \`make icon\` first"
  exit 0
fi

# 1. Overlay the bundle icon. The default file is electron.icns;
#    keep the original filename so Info.plist's CFBundleIconFile
#    reference stays intact.
cp -f "$ICON_SRC" "$RES/electron.icns"
echo "[setup-dev-icon] replaced $RES/electron.icns"

# 2. Retag the bundle so Force Quit / Cmd+Tab show "NOTBBG Terminal"
#    instead of "Electron". Use defaults for atomic plist edits;
#    PlistBuddy is also fine but requires escaping path quirks.
defaults write "$PLIST" CFBundleName "NOTBBG Terminal" 2>/dev/null || true
defaults write "$PLIST" CFBundleDisplayName "NOTBBG Terminal" 2>/dev/null || true
plutil -convert xml1 "$PLIST" 2>/dev/null || true

# 3. Bust the macOS icon cache for this .app. Without this the
#    Finder + Dock often keep showing the prior icon until logout.
touch "$APP"
# IconServices cache (best-effort; fails silently when LaunchServices
# isn't reachable, e.g. in CI).
/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister \
  -f "$APP" 2>/dev/null || true

echo "[setup-dev-icon] done — restart \`npm run electron\` to see the new icon"
