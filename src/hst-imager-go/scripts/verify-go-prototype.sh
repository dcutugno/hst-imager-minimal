#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "[1/5] Checking required list implementation markers in main.go"
grep -n 'case "list"' main.go >/dev/null
grep -n 'func handleList' main.go >/dev/null

echo "[2/5] Running unit tests"
go test ./...

echo "[3/5] Building binary"
go build ./...

echo "[4/5] Running smoke test for list"
LIST_OUT="$(go run . list)"
if [[ "$LIST_OUT" == *"not implemented yet"* ]]; then
  echo "ERROR: list command is still stubbed"
  exit 1
fi

echo "[5/5] Running smoke test for JSON mode"
JSON_OUT="$(go run . --format json list)"
if [[ "$JSON_OUT" != *'"drives"'* ]]; then
  echo "ERROR: json list output does not contain drives"
  exit 1
fi

echo "OK: Go prototype list + json implementation is active and working."
