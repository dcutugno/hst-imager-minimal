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
- archive listing/extract now supports native non-zip formats in pure Go (`.tar`, `.tgz`, `.tar.gz`, `.tar.xz`, `.txz`, `.tar.bz2`, `.tbz2`, `.lha/.lzh`, `.lzx`, `.rar`, `.gz`, `.xz`, `.bz2`, `.z`), with `bsdtar` fallback for unsupported legacy variants
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

## Differential parity check (Go vs legacy backend)
Run a command/file-tree parity harness against the published legacy backend:

```bash
cd /Users/davide/Downloads/Git-Sources/hst-imager-minimal/src/hst-imager-go
./scripts/verify-go-legacy-parity.sh
```

Strict byte-level parity pack (deep fixtures + strict hash + zero skips):
```bash
./scripts/verify-go-byte-parity-pack.sh
```

CLI shape matrix parity (auto-generated from .NET command factories):
```bash
./scripts/verify-go-cli-shape-matrix.sh
```

Nightly long-haul parity (with artifact capture):
```bash
./scripts/run-nightly-parity.sh
```

Notes:
- Set `HST_IMAGER_LEGACY_BIN` to override legacy binary location.
- Set `HST_PARITY_HEAVY=1` to include heavy large-image write parity cases.
- Set `HST_PARITY_ERROR_MATRIX=1|0` to enable/disable broad missing-argument/error-path differential checks (default `1`).
- Set `HST_PARITY_FUZZ_ROUNDS=<N>` and optional `HST_PARITY_FUZZ_SEED=<seed>` to run deterministic randomized workflow differential checks (default `0` rounds).
- Set `HST_PARITY_FUZZ_STRICT_HASH=1` to enable raw media hash checks in fuzz workflows (disabled by default; semantic fuzz parity is still validated).
- Set `HST_PARITY_DEEP_FS=1` to enable deep archive/filesystem fixtures (special chars and nested trees).
- Set `HST_PARITY_ARTIFACT_DIR=<path>` to collect logs and failure artifacts for nightly runs.
- On failures, the script prints a temp directory containing `.diff` artifacts for each failed case.

Cross-platform automation:
- GitHub Actions workflow `/Users/davide/Downloads/Git-Sources/hst-imager-minimal/.github/workflows/go-legacy-parity.yml` runs baseline parity on Linux/macOS, smoke checks on Windows, and a non-blocking deep differential report job.

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
- `fs copy`/`fs extract` now read source UAE metadata in pure Go and propagate metadata properties (protection bits/comments) to destination metadata files, including entries where names do not require remapping.
- `fs dir`/`fs copy`/`fs extract` now resolve Amiga-style source path components via UAE metadata mappings (e.g. `file1*` -> `__uae___file1_` or `file1%2a`) in pure-Go mode.
- archive handling now supports native non-zip tar-family formats in Go (`.tar`, `.tgz`, `.tar.gz`, `.tar.xz`, `.txz`, `.tar.bz2`, `.tbz2`) for both `archive list` and `fs extract`.
- single-stream compressed archives now have native pure-Go handling for `archive list` and `fs extract` (`.gz`, `.xz`, `.bz2`, `.z`) with inner-path matching semantics.
- `fs extract` archive-root behavior now matches legacy semantics: when inner path is empty, extraction is recursive even without `--recursive`.
- archive path extraction now handles exact single-file inner paths and case-insensitive matching (`archive\Dir\File`).
- TAR/ZIP archive symlinks are exposed in JSON metadata and preserved by `fs extract`/host copy in pure-Go mode.
- LHA now has native `archive list` and native `fs extract` for mixed archives, including compressed `-lh4-/-lh5-/-lh6-/-lh7-` and stored `-lh0-` through vendored `koron-go/lha`.
- LZX now has native `archive list` and native `fs extract`, including compressed mode (`pack_mode=2`) and merged-file groups.
- RAR now has native `archive list` and native `fs extract` for standard single-archive inputs.
- imaging read/write now supports native `.xz` and `.rar` source streams in pure-Go mode.
- `bsdtar` fallback remains for unsupported/unknown LHA header variants and any legacy archive formats not yet implemented natively.
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
