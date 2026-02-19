# hst-imager-go (prototype)

Initial bootstrap for porting Hst Imager Console to Go.

## What it includes
- CLI structure with a high-level compatible command tree
- placeholder global options (`--verbose`, `--log-file`, `--format`)
- subcommand tree for `mbr`, `gpt`, `rdb`, `fs`, `adf`, `archive`, `settings`
- basic working file-based commands for early testing:
  - `blank <path> <size>`
  - `info <path>`
  - `transfer <source> <destination> [--size <bytes>]`
  - `read` and `write` as transfer aliases
  - `compare <source> <destination> [--size <bytes>]`
- automated tests for command tree, global option parsing, and end-to-end file flow

## Build
```bash
go build ./...
```

## Test
```bash
go test ./...
```

## Status
This is still a prototype: many command groups are currently stubs.
Core file-based flow is now executable and testable while maintaining a CLI-compatibility-first approach.
