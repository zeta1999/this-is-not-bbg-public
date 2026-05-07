//! notbbg-pluginsdk — Rust port of `libs/pluginsdk` (Go).
//!
//! Plugins are standalone binaries that talk to the notbbg server
//! over JSON-per-line on stdin/stdout. This crate gives Rust authors
//! the same surface the Go SDK exposes:
//!
//! * `Plugin::new("plugin.<name>.screen")` — wrap stdin/stdout.
//! * `Plugin::run(handler)` — main loop calling `handler(msg)` per
//!   incoming line.
//! * `Plugin::update_screen` / `update_cell_grid` — emit screen
//!   updates back to the server.
//! * `Plugin::publish` — emit an arbitrary `(topic, payload)` frame.
//!
//! The cell-model types (`Cell`, `CellAddress`, `CellStyle`,
//! `EnumOption`, `ChartSeries`, `TableColumn`) mirror the Go shapes
//! one-to-one and serialise to the same JSON layout — drop-in
//! interop with the existing TUI / desktop / phone renderers.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::io::{self, BufRead, BufReader, Stdin, Stdout, Write};

pub mod cells;

pub use cells::*;

/// One inbound bus message from the server.
#[derive(Debug, Deserialize)]
pub struct Message {
    #[serde(rename = "Topic")]
    pub topic: String,
    #[serde(rename = "Payload")]
    pub payload: Value,
}

/// Outbound envelope. Mirrors what `pluginsdk.Plugin.Publish` emits.
#[derive(Debug, Serialize)]
struct Outbound<'a> {
    #[serde(rename = "Topic")]
    topic: &'a str,
    #[serde(rename = "Payload")]
    payload: Value,
}

/// One styled line for the legacy `ScreenUpdate` shape.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StyledLine {
    pub text: String,
    pub style: String, // "header", "normal", "green", "red", "dim", "warn"
}

/// Legacy screen-update payload.
#[derive(Debug, Serialize)]
struct ScreenUpdate<'a> {
    screen_id: &'a str,
    lines: &'a [StyledLine],
}

/// Cell-grid screen update — the Phase-2 replacement for
/// `ScreenUpdate`. Renderers prefer this when `version` is set.
#[derive(Debug, Serialize)]
struct CellGridUpdate<'a> {
    screen_id: &'a str,
    cells: &'a [Cell],
    #[serde(skip_serializing_if = "is_false")]
    full_replace: bool,
    version: &'static str,
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(v: &bool) -> bool { !*v }

/// The plugin runtime — stdio framing + helpers.
pub struct Plugin {
    screen_topic: String,
    reader: BufReader<Stdin>,
    writer: Stdout,
}

impl Plugin {
    /// Construct a plugin bound to `screen_topic`. Holds stdin and
    /// stdout open for the program's lifetime.
    pub fn new(screen_topic: impl Into<String>) -> Self {
        Self {
            screen_topic: screen_topic.into(),
            reader: BufReader::new(io::stdin()),
            writer: io::stdout(),
        }
    }

    /// Block waiting for the next stdin frame and decode it. Returns
    /// `Ok(None)` when stdin closes (server disconnect).
    pub fn read(&mut self) -> io::Result<Option<Message>> {
        let mut buf = String::new();
        let n = self.reader.read_line(&mut buf)?;
        if n == 0 {
            return Ok(None);
        }
        let trimmed = buf.trim_end_matches(&['\n', '\r'][..]);
        if trimmed.is_empty() {
            return Ok(Some(Message {
                topic: String::new(),
                payload: Value::Null,
            }));
        }
        let msg: Message = serde_json::from_str(trimmed).map_err(|e| {
            io::Error::new(io::ErrorKind::InvalidData, format!("parse stdin: {e}"))
        })?;
        Ok(Some(msg))
    }

    /// Emit a legacy `ScreenUpdate` to the screen topic.
    pub fn update_screen(&mut self, screen_id: &str, lines: &[StyledLine]) {
        let payload = serde_json::to_value(ScreenUpdate { screen_id, lines })
            .unwrap_or(Value::Null);
        self.publish_value(&self.screen_topic.clone(), payload);
    }

    /// Emit a `CellGridUpdate` to the screen topic.
    pub fn update_cell_grid(&mut self, screen_id: &str, cells: &[Cell], full_replace: bool) {
        let payload = serde_json::to_value(CellGridUpdate {
            screen_id,
            cells,
            full_replace,
            version: "cellgrid/v1",
        })
        .unwrap_or(Value::Null);
        self.publish_value(&self.screen_topic.clone(), payload);
    }

    /// Emit an arbitrary `(topic, payload)` frame. Mirrors the Go
    /// SDK's `Publish` — used for output_topics or for one-off
    /// signalling outside the screen surface.
    pub fn publish<T: Serialize>(&mut self, topic: &str, payload: &T) {
        let v = serde_json::to_value(payload).unwrap_or(Value::Null);
        self.publish_value(topic, v);
    }

    fn publish_value(&mut self, topic: &str, payload: Value) {
        let env = Outbound { topic, payload };
        if let Ok(s) = serde_json::to_string(&env) {
            // Best-effort writes — a closed stdout means the server
            // is gone and the next `read()` will surface that.
            let _ = writeln!(self.writer, "{s}");
            let _ = self.writer.flush();
        }
    }

    /// Main loop. Calls `handler` for each inbound message; returns
    /// when stdin closes or read fails.
    pub fn run<F: FnMut(&mut Plugin, Message)>(mut self, mut handler: F) {
        loop {
            match self.read() {
                Ok(Some(msg)) => handler(&mut self, msg),
                Ok(None) => return,
                Err(_) => return,
            }
        }
    }
}

/// Convenience: try to interpret an inbound message as an
/// `InputEvent` from the plugin's `.input` topic. Returns `None`
/// when the payload doesn't decode.
pub fn as_input_event(msg: &Message) -> Option<InputEvent> {
    serde_json::from_value(msg.payload.clone()).ok()
}
