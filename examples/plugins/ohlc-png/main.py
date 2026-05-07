"""ohlc-png — Phase-9 Python plugin demo.

Reads JSON-per-line OHLC messages from stdin (notbbg server pipes
the `ohlc.binance.BTCUSDT` topic), keeps a rolling 200-bar window,
renders to PNG every PLOT_REFRESH_BARS bars (default 1 — refresh
on every new bar), and emits a CellGridUpdate with one image cell
that GUIs render via `NOTBBG:/abs/path` resolution.

Output protocol matches `libs/pluginsdk/sdk.go`:
  {"Topic": "plugin.ohlc-png.screen", "Payload": {ScreenUpdate-or-CellGrid-JSON}}

Cell grid shape mirrors `pluginsdk.ImageCell` so the existing
desktop / phone / TUI renderers (Phase-4 image cells) consume it
without change.
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import time
from collections import deque
from pathlib import Path

WINDOW = 200
SCREEN_TOPIC = "plugin.ohlc-png.screen"
SCREEN_ID = "PLOT"
REFRESH_BARS = int(os.environ.get("PLOT_REFRESH_BARS", "1"))
OUT_DIR = Path(os.environ.get("PLOT_OUT_DIR", tempfile.gettempdir()))
HEADLESS = os.environ.get("OHLC_PNG_HEADLESS", "0") == "1"


def emit(payload: dict) -> None:
    """Single source of truth for stdout framing — keeps the protocol
    in lockstep with libs/pluginsdk/sdk.go's UpdateScreen / Publish."""
    msg = {"Topic": SCREEN_TOPIC, "Payload": payload}
    sys.stdout.write(json.dumps(msg) + "\n")
    sys.stdout.flush()


def emit_image_cell(image_path: str, n_bars: int) -> None:
    cells = [
        {
            "address": {"row": 0, "col": 0},
            "type": "header",
            "text": "OHLC PNG (matplotlib)",
            "style": {"bold": True},
        },
        {
            "address": {"row": 1, "col": 0},
            "type": "text",
            "text": f"BTCUSDT — last {n_bars} bars",
            "style": {"fg": "dim"},
        },
        {
            "address": {"row": 2, "col": 0},
            "type": "image",
            "src": f"NOTBBG:{image_path}",
            "alt": f"BTCUSDT OHLC ({n_bars} bars)",
            "width": 640,
            "height": 320,
            "col_span": 4,
        },
    ]
    emit({
        "screen_id": SCREEN_ID,
        "cells": cells,
        "full_replace": True,
        "version": "cellgrid/v1",
    })


def emit_waiting() -> None:
    emit({
        "screen_id": SCREEN_ID,
        "cells": [
            {
                "address": {"row": 0, "col": 0},
                "type": "header",
                "text": "OHLC PNG",
                "style": {"bold": True},
            },
            {
                "address": {"row": 1, "col": 0},
                "type": "text",
                "text": "Waiting for ohlc.binance.BTCUSDT…",
                "style": {"fg": "dim"},
            },
        ],
        "full_replace": True,
        "version": "cellgrid/v1",
    })


def render_png(closes: list[float]) -> str | None:
    """Render the rolling-close series to a PNG file under OUT_DIR.
    Returns the absolute path or None if matplotlib isn't available
    (operator hasn't created the venv yet — plugin still emits a
    text-only PLOT tab so the operator sees a 'matplotlib missing'
    cell instead of a silent failure)."""
    try:
        import matplotlib  # type: ignore
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt  # type: ignore
    except ImportError:
        return None

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    out = OUT_DIR / f"ohlc-png-{int(time.time() * 1000)}.png"
    fig, ax = plt.subplots(figsize=(6.4, 3.2))
    ax.plot(closes, color="#3b82f6", linewidth=1.0)
    ax.set_title("BTCUSDT close (rolling)")
    ax.set_xlabel("bar")
    ax.set_ylabel("close")
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(out, dpi=80)
    plt.close(fig)
    return str(out)


def emit_no_matplotlib() -> None:
    emit({
        "screen_id": SCREEN_ID,
        "cells": [
            {
                "address": {"row": 0, "col": 0},
                "type": "header",
                "text": "OHLC PNG",
                "style": {"bold": True},
            },
            {
                "address": {"row": 1, "col": 0},
                "type": "text",
                "text": "matplotlib not installed — see README.md",
                "style": {"fg": "red"},
            },
        ],
        "full_replace": True,
        "version": "cellgrid/v1",
    })


def main() -> int:
    closes: deque[float] = deque(maxlen=WINDOW)
    bars_since_render = 0
    matplotlib_ok = True

    emit_waiting()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue

        payload_raw = msg.get("Payload")
        if payload_raw is None:
            continue
        if isinstance(payload_raw, str):
            try:
                payload = json.loads(payload_raw)
            except json.JSONDecodeError:
                continue
        else:
            payload = payload_raw

        # Server publishes feeds.OHLC — fields are CamelCase from Go's
        # json.Marshal (no struct tags). Filter on instrument so the
        # plugin remains correct even if the operator widens
        # input_topics to ohlc.binance.*.
        if payload.get("Instrument") != "BTCUSDT":
            continue
        close = payload.get("Close")
        if not isinstance(close, (int, float)) or close <= 0:
            continue

        closes.append(float(close))
        bars_since_render += 1
        if bars_since_render < REFRESH_BARS:
            continue
        bars_since_render = 0

        if not matplotlib_ok:
            continue
        path = render_png(list(closes))
        if path is None:
            matplotlib_ok = False
            emit_no_matplotlib()
            continue
        emit_image_cell(path, len(closes))

        if HEADLESS:
            return 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
