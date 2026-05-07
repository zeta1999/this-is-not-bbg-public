# formulettes-runner

Phase-8 generic-plugin demo. Shows that a notbbg plugin can shell
out to an arbitrary external CLI and surface results in the cell
grid without any change to the server / TUI / desktop / phone core.

The CLI here is `bs-cli`, a tiny C++ wrapper around
[`Sibelius::BS()`](../../../../Sibelius/formulettes/BlackScholes.h).
On every input edit (spot / strike / vol / rate / time / type), the
plugin spawns `./bs-cli` with the values as argv and parses one
JSON line: `{"price":…,"delta":…,"gamma":…,"vega":…,"theta":…,"rho":…}`.

## Build

```sh
cd examples/plugins/formulettes-runner
make           # builds both bs-cli (C++) and formulettes-runner (Go)
```

The Makefile assumes the formulettes header lives at
`../../../../Sibelius/formulettes/BlackScholes.h` (i.e. the
`Sibelius` repo is checked out as a sibling of `this-is-not-bbg`).
Override with `make FORM_DIR=/your/path` if it isn't.

## Install

```sh
notbbg plugin install examples/plugins/formulettes-runner
```

The reconcile loop picks the new plugin up within ~5 s. Open the
**FORM** tab in TUI / desktop / phone, edit any input cell — the
output cells (price + 5 greeks) refresh from the next bs-cli invocation.

## Scope cuts (deliberate, MVP)

- **One option type** (vanilla European call/put) — barriers, baskets,
  quantos etc are all in `formulettes/` but each needs its own CLI
  wrapper + dropdown entry. Mechanical extension.
- **No GPU calls.** formulettes is CPU-only here; the GPU path lives
  in `bt-engine` and ships as a separate plugin.
- **No formula-engine cells.** The plugin owns the math; the cell
  grid just shows numbers.
- **Greeks via finite differences** (delta / gamma / theta / rho) since
  `BlackScholes.h` only exposes `BS()` and `BSVega()` directly.
  Centered differences for delta/gamma keep accuracy ≥ 1e-6 over the
  default-config inputs.

## OSS-isolation

Per `project_oss_plugin_isolation.md`: every file specific to this
plugin lives in this directory. The C++ wrapper, Makefile, manifest,
go.mod, and Go entrypoint are all self-contained. The only outside
dependency is the formulettes header, included by relative path —
operators packaging the OSS release can either vendor a snapshot
into `cpp/` or document the sibling-checkout requirement.

## Tests

```sh
go test -race ./...
```

Tests stub `bs-cli` with a /bin/sh script via `BS_CLI_PATH`, so they
don't require the C++ build to have run.
