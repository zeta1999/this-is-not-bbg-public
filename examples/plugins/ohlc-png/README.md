# ohlc-png

Phase-9 Python-plugin demo. Subscribes to `ohlc.binance.BTCUSDT`,
keeps a rolling 200-bar window, renders the close series to a PNG
via matplotlib, and emits an `image` cell pointing at the file via
`NOTBBG:/abs/path` — the desktop / phone / TUI Phase-4 image cell
renderers consume it without change.

## Setup

1. **Install the plugin** into the notbbg plugin home (the path
   `notbbg-server` runs from):
   ```sh
   notbbg plugin install examples/plugins/ohlc-png
   ```

2. **Create the per-plugin venv**:
   ```sh
   cd ~/.config/this-is-not-bbg/plugins/ohlc-png
   python3 -m venv .venv
   .venv/bin/pip install matplotlib
   ```
   `.venv/bin/python main.py` is the manifest's `command + args`,
   so the server picks the venv interpreter automatically.

3. **Reload** so the server's reconcile loop picks up the new venv:
   ```sh
   notbbg plugin reload ohlc-png
   ```

## Tunables

Set in the operator's environment (or in a wrapper shell script
referenced from the manifest):

- `PLOT_REFRESH_BARS` — re-render every N bars. Default `1` (every
  bar). Bump to `5` if matplotlib's render becomes a hot path.
- `PLOT_OUT_DIR` — directory for PNG outputs. Default
  `$TMPDIR` (macOS) / `/tmp` (Linux).
- `OHLC_PNG_HEADLESS` — set to `1` to exit after the first render
  (used by the test harness; not for production).

## How the GUIs render the image

- **Desktop**: inline `<img src="file:///tmp/ohlc-png-…">` (the
  `NOTBBG:` prefix is rewritten by the desktop renderer).
- **Phone**: text placeholder `📎 [IMG alt] src` (sixel/file://
  rendering not yet wired on RN).
- **TUI**: text placeholder `📎 [IMG alt] /tmp/…` until terminal
  graphics (sixel/kitty) lands.

## Tests

```sh
go test -race ./...
```

Spawns the Python plugin, asserts:
- The first stdout frame is the "Waiting…" cell grid.
- Feeding one canned BTCUSDT OHLC frame triggers either an
  `image` cell (matplotlib present) OR the
  "matplotlib not installed" red fallback (matplotlib absent).
  Either is a load-bearing observable output — both prove the
  plugin handled the bar correctly.

Tests skip cleanly when `python3` isn't on PATH.

## OSS-isolation

Per `project_oss_plugin_isolation.md`: `manifest.yaml`, `main.py`,
the venv, and the test live in this directory. No notbbg-core
file is touched.

## Next: Phase-9 agent integration

Per the NEXT-STEPS plan, the natural follow-up is an Agent tab
that hands the rendered PNG path to `claude -p` and streams the
summary back into the agent terminal. Defer that until this
plugin has been smoke-tested live.
