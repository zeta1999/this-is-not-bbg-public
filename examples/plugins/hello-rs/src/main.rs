//! hello-rs — Phase-10 Rust plugin port of `examples/plugins/hello-world/`.
//!
//! Subscribes to `ohlc.binance.*`, filters for BTCUSDT, and renders
//! a tiny cell grid showing the latest close price + a one-line
//! "last update" timestamp. Validates that the Rust SDK can do the
//! same job as the Go SDK without server / TUI / desktop changes.

use notbbg_pluginsdk::{
    cells::{header_cell, number_cell, text_cell},
    Plugin,
};
use serde::Deserialize;

#[derive(Deserialize)]
struct OHLC {
    #[serde(rename = "Instrument")]
    instrument: String,
    #[serde(rename = "Close")]
    close: f64,
    #[serde(rename = "Timeframe", default)]
    timeframe: String,
}

fn main() {
    let mut p = Plugin::new("plugin.hello-rs.screen");

    // Initial waiting frame so the HELLORS tab is populated cold.
    p.update_cell_grid(
        "HELLORS",
        &[
            header_cell(0, 0, "HELLO RUST"),
            text_cell(1, 0, "Waiting for BTCUSDT…"),
        ],
        true,
    );

    let mut last_close: Option<f64> = None;
    let mut last_tf = String::new();

    p.run(|p, msg| {
        let payload: OHLC = match serde_json::from_value(msg.payload.clone()) {
            Ok(v) => v,
            Err(_) => return,
        };
        if payload.instrument != "BTCUSDT" {
            return;
        }
        last_close = Some(payload.close);
        last_tf = payload.timeframe.clone();
        p.update_cell_grid(
            "HELLORS",
            &[
                header_cell(0, 0, "HELLO RUST — BTCUSDT"),
                text_cell(1, 0, format!("source: ohlc.binance ({})", last_tf)),
                number_cell(2, 0, "Close", last_close.unwrap_or_default(), 2, "$"),
            ],
            true,
        );
    });
}
