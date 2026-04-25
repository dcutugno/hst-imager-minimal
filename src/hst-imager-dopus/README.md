# Hst Imager DOpus

Cross-platform DOpus 4.x-inspired dual-pane file manager for `hst-imager-go`.

This app is intentionally a shell around the Go engine. Browsing and mutating file operations are routed through `hst-imager-go`; the GUI does not silently fall back to native OS copy/delete/rename behavior.

## Stack

- Tauri 2 desktop shell
- Rust backend for sidecar process management
- Svelte + TypeScript frontend
- `hst-imager-go --format json` as the operation engine

## Development

```sh
cd src/hst-imager-dopus
npm install
npm run prepare-engine
npm run dev
```

Use an existing local engine build:

```sh
HST_DOPUS_ENGINE_PATH=/path/to/hst-imager-go npm run dev
```

## Operations

Implemented command buttons:

- `Copy`: routes to `fs copy`
- `Extract`: routes to `fs extract`
- `Mkdir`: routes to `fs mkdir`
- `Info`: routes to `info`
- `Reread`: reloads the active pane
- `Swap`: swaps source/destination panes
- `Buttons`: edits the command bank JSON in memory

`Delete` and `Rename` are wired to `fs delete`/`fs rename` but disabled in the default button bank until the user explicitly enables them. In pure Go they currently support local host filesystem targets; virtual Amiga filesystem mutation paths still require the legacy bridge.

## Smoke Tests

```sh
npm run smoke
```

The smoke script builds or reuses `hst-imager-go`, then verifies:

- local folder browsing through `fs dir`
- archive fixture browsing when a fixture is present
- RDB image fixture browsing when a fixture is present
- `fs mkdir`
- `fs copy`
- `fs rename`
- `fs delete`
- invalid path error propagation

For generated PiStorm/Emu68-style hybrid image browsing:

```sh
HST_DOPUS_HEAVY_SMOKE=1 npm run smoke
```

That path creates a deterministic sparse image with `blank` + `format pistorm` and verifies MBR/RDB-style navigation entries.
