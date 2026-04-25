#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Prototype verification must validate pure-Go handlers directly.
export HST_IMAGER_LEGACY_MODE=off

echo "[1/7] Checking required list implementation markers in main.go"
grep -n 'case "list"' main.go >/dev/null
grep -n 'func handleList' main.go >/dev/null

echo "[2/7] Running unit tests"
go test ./...

echo "[3/7] Building binary"
GO_EXE="$(go env GOEXE 2>/dev/null || true)"
mkdir -p .bin
go build -o ".bin/hst-imager-go${GO_EXE}" .

echo "[4/7] Running smoke test for list"
LIST_OUT="$(go run . list)"
if [[ "$LIST_OUT" == *"not implemented yet"* ]]; then
  echo "ERROR: list command is still stubbed"
  exit 1
fi

echo "[5/7] Running smoke test for JSON mode"
JSON_OUT="$(go run . --format json list)"
if [[ "$JSON_OUT" != *'"drives"'* ]]; then
  echo "ERROR: json list output does not contain drives"
  exit 1
fi

LZW_PATH="../Hst.Imager.Core.Tests/TestData/Lzw/test.txt.Z"

echo "[6/7] Running smoke test for native LZW archive list"
LZW_LIST_OUT="$(go run . archive list "$LZW_PATH")"
if [[ "$LZW_LIST_OUT" != *"test.txt"* ]]; then
  echo "ERROR: lzw archive list output does not contain test.txt"
  exit 1
fi

echo "[7/7] Running smoke test for native LZW extract"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
go run . fs extract "$LZW_PATH" "$TMP_DIR" --recursive >/dev/null
if [[ ! -s "$TMP_DIR/test.txt" ]]; then
  echo "ERROR: lzw extract did not produce non-empty test.txt"
  exit 1
fi

echo "OK: Go prototype list + json + lzw implementation is active and working."
