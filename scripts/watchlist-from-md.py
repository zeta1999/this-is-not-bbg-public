#!/usr/bin/env python3
"""watchlist-from-md.py — turn a propaganda/investment markdown
watchlist into server/configs/watchlist.yaml.

The markdown shape this parses:

    ## Theme N — <theme name>
    ...
    ### <Company Name> — <ticker> (<exchange>, <country>)
    - **Theme angle:** ...
    - **Recent articles:**
      - [<title> — <publisher>, <date>](<url>)

Each ### company block becomes one watchlist entry. Theme is taken
from the most recent ## heading. The yahoo symbol is derived from
the ticker + exchange tag using the standard Yahoo Finance suffix
table; news_feeds defaults to a Google News RSS query because the
Recent-articles links are individual articles, not feeds.

Usage:
  ./scripts/watchlist-from-md.py <path-to-md> > server/configs/watchlist.yaml

Or to overwrite in place (with backup):
  ./scripts/watchlist-from-md.py <path-to-md> --write
"""
from __future__ import annotations

import argparse
import os
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path

# Exchange tag → Yahoo Finance suffix. Bare US tickers (NYSE/Nasdaq)
# need no suffix; everything else does. Match is case-insensitive
# and substring-aware so "Nasdaq Stockholm" is recognised as STO and
# "SSE STAR" is recognised as SSE.
EXCHANGE_SUFFIX = {
    "NYSE": "",
    "NASDAQ": "",
    "AMEX": "",
    "TSE": ".T",                # Tokyo
    "KOSPI": ".KS",
    "KOSDAQ": ".KQ",
    "HKEX": ".HK",
    "SEHK": ".HK",
    "STO": ".ST",               # Stockholm
    "NASDAQ STOCKHOLM": ".ST",
    "BME": ".MC",               # Madrid
    "XETRA": ".DE",
    "LSE": ".L",
    "SSE": ".SS",                # Shanghai (incl. STAR market)
    "SZSE": ".SZ",               # Shenzhen
    "EURONEXT": ".PA",           # Paris default
    "ASX": ".AX",
    "TSX": ".TO",
}

HEADING_RE = re.compile(r"^##\s+Theme\s+\d+\s*[—-]\s*(.+?)\s*$")
COMPANY_RE = re.compile(
    r"^###\s+(?P<name>.+?)\s*[—-]\s*(?P<ticker>[A-Z0-9.]+)\s*\((?P<exch>[^)]+)\)"
)


@dataclass
class Company:
    name: str
    ticker: str
    yahoo: str
    theme: str = ""
    news_feeds: list[str] = field(default_factory=list)


def google_news_rss(query: str) -> str:
    # Quote+plus form is what Google News RSS expects.
    from urllib.parse import quote_plus
    return f"https://news.google.com/rss/search?q={quote_plus(query)}&hl=en-US&gl=US&ceid=US:en"


def resolve_yahoo(ticker: str, exch_tag: str) -> str:
    """Map a (ticker, "TSE, JP" / "NYSE, US" / …) pair to Yahoo's symbol."""
    # Strip trailing dot some sources use ("NG.").
    ticker = ticker.rstrip(".")
    # If ticker already carries a Yahoo-style suffix, trust it.
    if "." in ticker and ticker.split(".")[-1] in {
        "T", "KS", "KQ", "HK", "ST", "MC", "DE", "L", "SS", "SZ", "PA",
        "AX", "TO", "AS", "BR",
    }:
        return ticker
    # First chunk before the comma is the exchange tag. Match
    # case-insensitively and pick the longest table key that's a
    # substring — handles "Nasdaq Stockholm" → STO before "Nasdaq"
    # → US, and "SSE STAR" → SSE.
    head = exch_tag.split(",")[0].strip().upper()
    best_key = ""
    for k in EXCHANGE_SUFFIX:
        if k in head and len(k) > len(best_key):
            best_key = k
    if not best_key:
        print(
            f"warn: unknown exchange '{head}' for {ticker}; emitting bare ticker",
            file=sys.stderr,
        )
        return ticker
    return ticker + EXCHANGE_SUFFIX[best_key]


def parse(text: str) -> list[Company]:
    theme = ""
    out: list[Company] = []
    for line in text.splitlines():
        if m := HEADING_RE.match(line):
            theme = m.group(1).strip()
            continue
        if m := COMPANY_RE.match(line):
            name = m.group("name").strip()
            ticker = m.group("ticker").strip()
            exch = m.group("exch").strip()
            yahoo = resolve_yahoo(ticker, exch)
            news = [google_news_rss(f"{name} {ticker}")]
            out.append(Company(
                name=name, ticker=ticker, yahoo=yahoo,
                theme=theme, news_feeds=news,
            ))
    return out


def emit_yaml(companies: list[Company], src_path: str) -> str:
    lines: list[str] = []
    lines.append("# watchlist.yaml — auto-generated from")
    lines.append(f"#   {src_path}")
    lines.append("#")
    lines.append("# Regenerate with scripts/watchlist-from-md.py.")
    lines.append("# Reset to the safe public default with scripts/watchlist-reset.sh.")
    lines.append("# This file is gitignored — personal trading info stays local.")
    lines.append("")
    lines.append("companies:")
    for c in companies:
        lines.append(f"  - name: {yaml_str(c.name)}")
        lines.append(f"    ticker: {yaml_str(c.ticker)}")
        lines.append(f"    yahoo: {yaml_str(c.yahoo)}")
        if c.theme:
            lines.append(f"    theme: {yaml_str(c.theme)}")
        if c.news_feeds:
            lines.append("    news_feeds:")
            for f in c.news_feeds:
                lines.append(f"      - {yaml_str(f)}")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def yaml_str(s: str) -> str:
    # Quote anything with characters YAML treats specially or that
    # could parse as a number/bool.
    if s == "" or any(c in s for c in ":#[]{},&*!|>'\"%@`?-"):
        escaped = s.replace("\\", "\\\\").replace('"', '\\"')
        return f'"{escaped}"'
    if s.lower() in {"true", "false", "yes", "no", "null", "~"}:
        return f'"{s}"'
    return s


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    ap.add_argument("md", help="path to a propaganda/investment markdown watchlist")
    ap.add_argument(
        "--write", action="store_true",
        help="overwrite server/configs/watchlist.yaml in place",
    )
    args = ap.parse_args()

    md_path = Path(args.md).expanduser().resolve()
    if not md_path.exists():
        print(f"error: {md_path} does not exist", file=sys.stderr)
        return 1

    text = md_path.read_text()
    companies = parse(text)
    if not companies:
        print("error: no companies parsed — check the markdown shape", file=sys.stderr)
        return 1

    yaml_text = emit_yaml(companies, str(md_path))

    if args.write:
        repo_root = Path(__file__).resolve().parent.parent
        dst = repo_root / "server/configs/watchlist.yaml"
        if dst.exists():
            backup = dst.with_suffix(".yaml.bak")
            dst.replace(backup)
            print(f"backup: {backup.relative_to(repo_root)}", file=sys.stderr)
        dst.write_text(yaml_text)
        print(
            f"wrote {dst.relative_to(repo_root)} ({len(companies)} companies)",
            file=sys.stderr,
        )
    else:
        sys.stdout.write(yaml_text)

    return 0


if __name__ == "__main__":
    sys.exit(main())
