# hst-imager-go (prototype)

Initial bootstrap for porting Hst Imager Console to Go.

## What it includes
- CLI structure with a high-level compatible command tree
- global options (`--verbose`, `--log-file`, `--format`) with `table|json` output modes
- working handlers for all top-level command groups and subcommands in the current Go command tree:
  - imaging: `list`, `blank`, `convert`, `transfer`, `read`, `write`, `compare`, `info`, `optimize`, `format`
  - block: `block read`, `block view`
  - settings: `settings list`, `settings update`
  - filesystem: `fs dir`, `fs copy`, `fs extract`, `fs mkdir`
  - archive/script/adf: `archive list`, `script`, `adf create`
  - partition tables:
    - `mbr info|initialize|part add|delete|format|export|import|clone`
    - `gpt info|initialize|part add|delete|format`
    - `rdb info|initialize|resize|filesystem add|delete|import|export|update|part add|update|delete|copy|export|import|kill|move|format|update|backup|restore`
- command aliases to match original CLI habits:
  - `init` -> `initialize`
  - `del` -> `delete`
  - `rdb fs ...` -> `rdb filesystem ...`
- automated tests for command tree, global option parsing, file flow, and expanded advanced command families
- transfer/read/write/compare support compressed image flows:
  - read from `.gz` and `.zip`
  - write to `.gz` and `.zip`
- archive listing/extract now supports non-zip formats via `bsdtar` fallback (including `.lha`/`.lzx` when available in runtime tar support)
- partition path support in I/O commands using `\mbr\N`, `\gpt\N`, `\rdb\N` suffixes
- `fs dir <media>\mbr|gpt|rdb` lists partition containers

## Build
```bash
go build ./...
```

## Test
```bash
go test ./...
```

## Status
This Go prototype now has functional coverage for all commands currently exposed in `command_tree.go`.
Low-level parity work is in progress:
- MBR operations (`info`, `initialize`, `part add/delete/export/import/clone`) read/write real on-disk MBR sector structures.
- GPT operations (`info`, `initialize`, `part add/delete/format`) read/write real on-disk GPT headers and partition entries (including CRC updates and protective MBR).
- RDB operations now use an on-media binary RDB state block and embedded data regions for filesystems/partitions, including `backup`/`restore` from raw RDB bytes.
- `info <path>` now inspects and reports detected partition-table structures (MBR/GPT/RDB) instead of only file metadata.
- native Amiga `RDSK` partition-chain parsing is supported for `info`, `fs dir <media>\rdb`, and partition-path reads (`<media>\rdb\N`).
- native Amiga `RDSK` update-path support is available for `rdb part format/kill/move` and `rdb fs update` with checksum-correct block rewrites.

For strict parity against the original .NET engine, the Go CLI now supports a legacy backend bridge:
- `HST_IMAGER_LEGACY_MODE=off|auto|force` (default `auto`)
- `HST_IMAGER_LEGACY_BIN=<path-to-Hst.Imager.ConsoleApp(.dll)>` (optional; defaults to `/tmp/hst-imager-legacy`)

Behavior:
- `auto`: bridge parity-sensitive command families (imaging, block, settings, fs, archive, adf, mbr/gpt/rdb) and fall back to Go if legacy backend is unavailable.
- `force`: route every resolved command to legacy backend (no Go fallback), for maximum parity.

When enabled and available, commands are executed by the .NET backend for byte-identical legacy semantics (including UAE metadata behavior and deep filesystem/archive handling).

Pure-Go parity progress:
- `fs copy`/`fs extract` now write real UAE metadata file formats in Go:
  - `_UAEFSDB.___` as UAEFSDB v1 binary nodes (600-byte records with big-endian mode and fixed C-string fields).
  - `.uaem` files using the legacy text wire format (`<8 flags> <yyyy-MM-dd HH:mm:ss.ff> <comment>`).
- UAE filename mapping now follows legacy helper semantics for special characters and mode-specific naming (`uaefsdb` vs `uaemetafile`).
- `fs dir` now reads `_UAEFSDB.___` and `.uaem` metadata in pure Go to resolve Amiga names/protection bits/comments, hides metadata files from listings, and supports recursive metadata-aware path mapping.
- Added parity-focused tests for binary layout, protection-bit formatting, and filename encoding edge cases.

Example using your published artifacts:

```bash
cd /Users/davide/Downloads/Git-Sources/hst-imager-minimal/src/hst-imager-go
HST_IMAGER_LEGACY_MODE=force \
HST_IMAGER_LEGACY_BIN=/tmp/hst-imager-legacy/Hst.Imager.ConsoleApp.dll \
go run . fs copy <src> <dst> --recursive --uaemetadata uaefsdb
```

The bridge runs `dotnet` with `DOTNET_ROLL_FORWARD=Major`, so a newer installed runtime can execute a `net8.0` published backend.


## Verify local vs remote sync
If your cloned `main` still prints `Command 'list' is not implemented yet in Go prototype.`, run:

```bash
cd ~/Downloads/Git-Sources/hst-imager-minimal
git fetch origin
git reset --hard origin/main
git clean -fd
cd src/hst-imager-go
./scripts/verify-go-prototype.sh
```

The script fails fast if `list` is still stubbed or JSON mode is missing.
