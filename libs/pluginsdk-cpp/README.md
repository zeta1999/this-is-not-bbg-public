# notbbg-pluginsdk-cpp

C++17 port of [`libs/pluginsdk`](../pluginsdk/) — same JSON-per-line
stdin/stdout protocol, same cell-grid layout. A C++ plugin written
against this SDK is a drop-in peer of one written in Go or Rust.

## Design constraints

- **Zero external runtime deps.** The SDK serializes outbound cells
  with a small hand-rolled JSON helper; inbound payloads are exposed
  as a raw `std::string` so the plugin can pull in nlohmann /
  RapidJSON / etc only when *it* needs to parse a payload, not as
  part of the SDK contract.
- **Header + small impl**, not header-only — keeps build times sane
  for plugins that include the SDK in many translation units.
- **C++17**, broadest macOS / Linux toolchain compatibility.

## Building

```sh
cmake -B build -DNOTBBG_PLUGINSDK_TESTS=ON
cmake --build build
ctest --test-dir build --output-on-failure
```

The static library lands at `build/libnotbbg-pluginsdk.a`. Link it
into your plugin with the include path `libs/pluginsdk-cpp/include`.

## Quickstart

```cpp
#include "notbbg/pluginsdk.hpp"
#include <vector>

int main() {
    notbbg::Plugin p("plugin.my-plugin.screen");

    std::vector<notbbg::Cell> cells = {
        notbbg::header_cell(0, 0, "MY PLUGIN"),
        notbbg::number_cell(1, 0, "x", 42.0, 0, ""),
    };
    p.update_cell_grid("MYTAB", cells, /*full_replace=*/true);

    p.run([](notbbg::Plugin& p, const notbbg::Message& m) {
        // m.topic + m.payload_raw — parse however you like.
        // Then build a new vector of Cells and call update_cell_grid again.
    });
}
```

## What's covered

- `Plugin::read / run / update_cell_grid / publish_raw` — framing.
- `Cell` + `CellAddress` + `CellStyle` POD types.
- Cell builders: `text_cell`, `header_cell`, `number_cell`,
  `decimal_input_cell`. More land as plugins demand them.

## Not yet covered

- `update_screen` (legacy `StyledLine`) — add when a C++ plugin
  needs the pre-cell-grid surface (almost never, today).
- Formula engine port (`libs/pluginsdk/formula/`). The grammar is
  Excel-style with `R<row>C<col>` refs; same semantics as the Go
  package. Add when a C++ plugin wants `=BS(R0C0, …)`.
- Higher-level cell builders (chart, table, image, enum). Source
  pattern is mechanical — copy the existing helpers in `pluginsdk.hpp`.

## Testing

```sh
ctest --test-dir build --output-on-failure
```

Test binary at `build/pluginsdk_test`. New cell shapes should add a
case under `tests/pluginsdk_test.cpp` so the wire layout stays
byte-aligned with the Go and Rust SDKs (the renderers don't know
which language emitted a frame).

## Next: formulettes-runner migration

Per `todos/NEXT-STEPS.md` Phase-10 plan, the natural second
demonstration is migrating
[`examples/plugins/formulettes-runner/`](../../examples/plugins/formulettes-runner/)
from "Go plugin → spawns bs-cli" to "C++ plugin → uses this SDK
directly + computes BS in-process". That removes the spawn cost
per cell edit and validates the SDK against a real workload.
