# Hst Imager Console: minimal porting proposal (without Electron/Chrome)

## Objective
Port the console version features to a new **cross-platform** implementation while preserving feature parity and reducing startup time, memory usage, and distribution complexity.

## Recommended language
The recommended language is **Go (Golang)**.

### Main reasons
- **Single binary** per target platform (Windows, macOS, Linux).
- **Fast startup** and smaller footprint than browser-runtime-based stacks.
- **Native cross-compilation** that is straightforward to integrate in CI/CD.
- **Lightweight concurrency** for I/O-heavy pipelines (read/write/compare/transfer).
- **Mature CLI ecosystem** (`cobra`, `pflag`, `viper`) to preserve scripting UX.

## Suggested architecture

### 1) Core domain (CLI-independent)
Package `internal/core` with primitives and use cases:
- media/drive abstraction
- image abstraction (`img`, `hdf`, `vhd`)
- partition abstraction (`mbr`, `gpt`, `rdb`)
- filesystem abstraction and copy/extract/list operations

### 2) Adapter layer
Packages under `internal/adapters/...` for:
- local file I/O
- OS-specific raw device access
- streaming compression/decompression (`zip`, `gz`, `xz`, `rar`)
- bridge to existing C/.NET libraries where direct porting is not yet practical

### 3) CLI layer
Package `cmd/hst-imager` with a command tree compatible with the current one:
- `read`, `write`, `compare`, `transfer`, `blank`, `optimize`, `format`
- `fs`, `mbr`, `gpt`, `rdb`, `adf`, `archive`, `settings`

## Migration strategy (low risk)

### Phase 0: baseline and compatibility
- Freeze a command/output matrix from the current console.
- Define golden tests for textual output and exit codes.

### Phase 1: operational MVP
- Implement high-usage core commands in Go: `list/info/read/write/compare`.
- Reach parity for raw and VHD images.

### Phase 2: advanced partitioning
- Port `mbr` and `gpt`.
- Port `rdb` with binary regression tests on real fixtures.

### Phase 3: filesystem and archives
- Port `fs` and `archive`.
- Incrementally integrate compressed formats and edge cases.

### Phase 4: hardening and release
- Benchmark performance versus the current console.
- Build multi-OS/arch matrix and package binaries.
- Gradual rollout with a beta channel.

## Recommended dependencies
- CLI: `cobra`, `pflag`
- Logging: `zerolog` or `slog`
- Compression: stdlib + `xz` library
- Testing: `testify`, custom golden test harness

## How to keep 100% feature coverage
1. **Contract-first**: formalize CLI input/output and error semantics from the current version.
2. **Feature flags**: introduce Go subcommands by functional area and validate in parallel.
3. **Back-compat mode**: keep aliases, option names, and exit codes aligned.
4. **Real fixtures**: reuse scripts/examples and test images already in the repository.

## Main risks and mitigations
- **Raw disk access differs by OS** -> separate adapters and platform integration tests.
- **Legacy Amiga/RDB formats** -> incremental migration with binary regression tests.
- **CLI output parity** -> golden tests for help, errors, and table output.

## Execution

For an operational execution plan, see the [feature parity roadmap](go-porting-roadmap.md).

## Final recommendation
For this project, a **Go** port is the best choice, with an incremental test-driven approach that preserves current console commands and behavior.
