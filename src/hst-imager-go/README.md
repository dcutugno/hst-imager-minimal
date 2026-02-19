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
Implementation is intentionally lightweight in some advanced areas (metadata-backed partition-table operations, local-filesystem-focused fs behavior) and does not yet reproduce full binary-level parity with the original .NET core for every media/filesystem format.


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
