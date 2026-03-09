#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ARTIFACT_DIR="${HST_PARITY_ARTIFACT_DIR:-$ROOT_DIR/parity-artifacts}"
mkdir -p "$ARTIFACT_DIR"

LOG_FILE="$ARTIFACT_DIR/parity.log"
SUMMARY_FILE="$ARTIFACT_DIR/summary.txt"

set +e
HST_PARITY_DEEP_FS="${HST_PARITY_DEEP_FS:-1}" \
HST_PARITY_HEAVY="${HST_PARITY_HEAVY:-1}" \
HST_PARITY_FUZZ_ROUNDS="${HST_PARITY_FUZZ_ROUNDS:-24}" \
HST_PARITY_FUZZ_STRICT_HASH="${HST_PARITY_FUZZ_STRICT_HASH:-1}" \
HST_PARITY_REQUIRE_NO_SKIP="${HST_PARITY_REQUIRE_NO_SKIP:-1}" \
./scripts/verify-go-byte-parity-pack.sh 2>&1 | tee "$LOG_FILE"
rc=${PIPESTATUS[0]}
set -e

{
  echo "exit_code=$rc"
  grep -E '^Summary: ' "$LOG_FILE" | tail -n 1 || true
} >"$SUMMARY_FILE"

artifact_src="$(grep -Eo 'Detailed artifacts left in: .*$' "$LOG_FILE" | sed 's/^Detailed artifacts left in: //' | tail -n 1 || true)"
if [[ -n "$artifact_src" && -d "$artifact_src" ]]; then
  mkdir -p "$ARTIFACT_DIR/failure-artifacts"
  cp -R "$artifact_src"/. "$ARTIFACT_DIR/failure-artifacts/"
fi

exit "$rc"
