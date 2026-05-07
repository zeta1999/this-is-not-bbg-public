# notbbg-pluginsdk (Rust)

Rust port of [`libs/pluginsdk`](../pluginsdk/) — the same JSON-per-line
stdin/stdout protocol, the same cell-grid types, the same convenience
helpers. A plugin written against this crate is a drop-in peer of one
written in Go: server / TUI / desktop / phone consume both without
discrimination.

## Quickstart

```rust
use notbbg_pluginsdk::{cells::{header_cell, number_cell}, Plugin};

fn main() {
    let mut p = Plugin::new("plugin.my-plugin.screen");
    p.run(|p, msg| {
        // ... handle msg ...
        p.update_cell_grid(
            "MYTAB",
            &[
                header_cell(0, 0, "MY PLUGIN"),
                number_cell(1, 0, "x", 42.0, 0, ""),
            ],
            true, // full_replace
        );
    });
}
```

A complete example: [`examples/plugins/hello-rs`](../../examples/plugins/hello-rs).

## What's covered

- `Plugin::new / read / run` — stdin framing.
- `update_screen` (legacy `StyledLine`) and `update_cell_grid`
  (Phase-2 cell grid).
- `publish(topic, payload)` — emit on any `output_topics` from the
  manifest.
- `as_input_event` + `InputEvent::is_cancel` — same semantics as
  Go's `pluginsdk.AsInputEvent` + `IsCancel`.
- Cell builders: `text_cell`, `header_cell`, `number_cell`,
  `decimal_input_cell`, `integer_input_cell`, `enum_input_cell`,
  `image_cell`. More can be added as the porting demand surfaces.

## Not yet covered (Phase 10.b)

- Formula engine port (`libs/pluginsdk/formula/`). The grammar is
  Excel-style with `R<row>C<col>` refs; same surface as the Go
  package. Add when a Rust plugin needs `=BS(R0C0, R0C1, …)`.
- Long-running-job helpers (cancel-on-context); Rust idioms differ
  enough that the Go API doesn't translate verbatim — design pass
  needed.
- Higher-level cell builders that mirror every Go helper. Track the
  request as plugins land.

## Testing

```sh
cargo test
```

Covers cell-shape conformance against the Go SDK's wire layout. New
cell types should add a golden test under `tests/golden.rs` so a
field-name drift fails loud rather than reaching a renderer silently.

## OSS isolation

Per `project_oss_plugin_isolation.md`: the SDK lives under `libs/`
because it's the shared framing surface (proxied by the existing Go
SDK). Plugin-specific behavior must live in each plugin's own
directory.
