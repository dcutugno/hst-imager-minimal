# Hst Imager Console Go Porting: feature parity roadmap

This roadmap extends `go-porting-proposal.md` with an execution-oriented view: feature inventory, priorities, and measurable acceptance criteria.

## 1) Current command inventory (baseline)

### Root command and global options
- root: `hst-imager`
- global options: `--log-file`, `--verbose`, `--format`

### Top-level commands
- `blank`
- `convert` (legacy/obsolete)
- `transfer`
- `format`
- `info`
- `list`
- `optimize`
- `read`
- `script`
- `block read|view`
- `compare`
- `write`
- `gpt ...`
- `mbr ...`
- `rdb ...`
- `fs ...`
- `adf ...`
- `settings ...`

### `gpt` commands
- `gpt info`
- `gpt initialize`
- `gpt part add`
- `gpt part delete`
- `gpt part format`

### `mbr` commands
- `mbr info`
- `mbr initialize`
- `mbr part add`
- `mbr part delete`
- `mbr part format`
- `mbr part export`
- `mbr part import`
- `mbr part clone`

### `rdb` commands
- `rdb info`
- `rdb initialize`
- `rdb resize`
- `rdb filesystem add|delete|import|export|update`
- `rdb part add|update|delete|copy|export|import|kill|move|format`
- `rdb update`
- `rdb backup`
- `rdb restore`

### `fs` commands
- `fs dir`
- `fs copy`
- `fs extract`
- `fs mkdir`

### `adf`, `archive`, `settings` commands
- `adf create`
- `archive list`
- `settings list`
- `settings update`

## 2) Feature parity strategy

## Track A — CLI compatibility (highest priority)
**Goal:** allow replacing the current binary in CI/scripts without changing common command usage.

Acceptance criteria:
- same command tree for root + main groups
- compatible essential options (`--size`, `--verify`, `--force`, etc.)
- consistent success/failure exit codes

## Track B — Core data path (I/O image & device)
**Goal:** provide robust read/write/compare/transfer behavior.

Acceptance criteria:
- fixture-based tests and checksums
- throughput benchmarks on large files
- equivalent retry/error policy handling

## Track C — Partitioning & Amiga-specific
**Goal:** full coverage for MBR/GPT/RDB, including legacy scenarios.

Acceptance criteria:
- binary regression tests for headers/metadata
- validated import/export/copy/clone on real fixtures

## Track D — Filesystem & archive
**Goal:** parity for `fs` and `archive` in real workflows (zip/lha/adf/iso where relevant).

Acceptance criteria:
- end-to-end tests for copy/extract/mkdir across multiple backends
- content and tree-structure preservation validation

## 3) Recommended sequence (indicative 12 weeks)

### Sprint 1-2
- Go scaffolding (`cmd/`, `internal/`)
- compatible CLI parser for `list`, `info`, `read`, `write`, `compare`, `transfer`
- golden tests for help/usage

### Sprint 3-4
- `blank`, `optimize`, `format`
- global option policy (`--verbose`, `--format`)
- logging framework + error mapping + exit codes

### Sprint 5-7
- complete `mbr` and `gpt`
- binary regression fixture suite

### Sprint 8-10
- complete `rdb` (filesystem/part/update/backup/restore)
- hardening for Amiga edge cases

### Sprint 11
- `fs`, `adf`, `archive`, `settings`
- cross-platform integration tests

### Sprint 12
- final benchmarks
- multi-OS/arch packaging
- release candidate + rollback plan

## 4) Definition of Done per command
A command is considered "ported" when all criteria are met:
1. syntax compatible with current version
2. help output aligned and validated via golden tests
3. stable output (table/json where applicable)
4. aligned exit codes and error wording
5. real fixture tests + at least one failure-path test

## 5) Remaining risks
- OS-specific differences in raw device access
- full compatibility for legacy formats (especially RDB/PFS3)
- maintenance cost for temporary hybrid bridges during incremental migration

Mitigation: keep a dual-run phase (old vs new) in CI for critical commands with automated output/exit-code comparison.
