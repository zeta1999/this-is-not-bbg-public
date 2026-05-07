# backtester

Plugin demonstrating two operating modes:

- **RT (Live)** — subscribes to `ohlc.binance.*`, runs a built-in SMA
  crossover (or a user-edited Aria-DSL script) on the live stream,
  and updates the cell grid every bar.
- **Historical** — shells out to the
  [`bt-engine`](https://github.com/anthropic-ai/notbbg-private) binary
  with a generated TOML + Aria strategy, parses the
  `aggregate` block from its JSON summary, and surfaces
  pnl/sharpe/maxDD/trades as cells.

## Operator setup

1. **Build bt-engine** (Rust toolchain required):
   ```sh
   cd ~/work/gpu-backtest && cargo build --release
   ```
   Result: `~/work/gpu-backtest/target/release/bt-engine`.

2. **Point the plugin at the binary** (optional — auto-discovery
   probes `~/work/gpu-backtest/target/release/bt-engine` and
   `../gpu-backtest/target/release/bt-engine`):
   ```sh
   export BT_ENGINE_PATH=/path/to/bt-engine
   ```

3. **Point bt-engine at a DuckDB OHLC dataset** (defaults to
   `~/work/gpu-backtest/data/synth_oss.duckdb`; override per-deploy):
   ```sh
   export BT_ENGINE_DUCKDB_PATH=/path/to/your.duckdb
   ```
   bt-engine's `DataConfig` requires DuckDB — CSV / Parquet inputs
   would mean an extra ingest step. Use the gpu-backtest
   `scripts/prepare-all.sh` to bootstrap a sample DB.

4. **Install the plugin**:
   ```sh
   notbbg plugin install examples/plugins/backtester
   ```

## Workflow

- Open the **BACKTESTER** tab in TUI / desktop / phone.
- **Mode = RT**: pick a template (SMA Crossover / Mean Reversion /
  Momentum / Custom). Custom loads `.strat` / `.aria` files from
  `~/.config/this-is-not-bbg/scripts/`.
- **Mode = Historical**: fill `Start` / `End` (YYYY-MM-DD), wait for
  the `Backtest` progress cell to flip from "Running bt-engine..." to
  the results section. **`X` cancels** mid-run; `histResult.status`
  flips to `cancelled` and the bt-engine subprocess is reaped.

## Hard constraint — OSS isolation

Per `project_bt_engine.md`: bt-engine source / types / config /
flags must NOT leak into `server/`, `tui/`, `desktop/`, or
`phone/`. All bt-engine integration lives in this directory. If
something looks shareable, push it into `libs/pluginsdk` instead.

## Known gaps

- `bt-engine`'s upstream `main.rs` doesn't yet wire the
  `[output] json_summary` flag into a stdout writer — the plugin
  expects the JSON shape from `output.rs::write_summary_multi_ticker`.
  Until that's wired, the plugin's parse step will fail with the
  bt-engine stderr surfaced into the error cell. The unit tests
  (`historical_mode_test.go`) stub the binary to exercise the
  parsing path independently.
- No /api/v1/history → DuckDB ingest path yet — operator brings
  their own DuckDB. Ingest from the live datalake would land as a
  follow-up task.

## Tests

```sh
cd examples/plugins/backtester && go test -race ./...
```

Covers: TOML generation, parse-good, parse-bad, env-override,
stub-bt-engine end-to-end (skipped on Windows).
