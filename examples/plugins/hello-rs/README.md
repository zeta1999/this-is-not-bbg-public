# hello-rs

Rust port of `examples/plugins/hello-world/`. First validation that
the Rust SDK ([`libs/pluginsdk-rs`](../../../libs/pluginsdk-rs))
delivers the same plugin surface as the Go SDK.

## Build + install

```sh
cargo build --release
notbbg plugin install examples/plugins/hello-rs
# the manifest's `command: ./hello-rs` picks up the binary in
# the plugin directory; copy or symlink target/release/hello-rs in
# if your install workflow doesn't co-locate them.
```

## Smoke-test

```sh
echo '{"Topic":"ohlc.binance.BTCUSDT","Payload":{"Instrument":"BTCUSDT","Close":65432.10,"Timeframe":"1m"}}' | ./target/debug/hello-rs
```

Expected: two stdout lines (the initial "Waiting…" frame, then a
3-cell update with the close price). Both are `cellgrid/v1` payloads
keyed at `plugin.hello-rs.screen` — exactly the shape the existing
Go-SDK plugins emit.
