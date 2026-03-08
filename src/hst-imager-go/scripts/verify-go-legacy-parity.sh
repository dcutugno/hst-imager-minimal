#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GO_BIN="${GO_BIN:-$ROOT_DIR/hst-imager-go}"
LEGACY_BIN="${HST_IMAGER_LEGACY_BIN:-}"
HEAVY="${HST_PARITY_HEAVY:-0}"

if [[ -z "$LEGACY_BIN" ]]; then
  if [[ -x /tmp/hst-imager-legacy/Hst.Imager.ConsoleApp ]]; then
    LEGACY_BIN=/tmp/hst-imager-legacy/Hst.Imager.ConsoleApp
  elif [[ -f /tmp/hst-imager-legacy/Hst.Imager.ConsoleApp.dll ]]; then
    LEGACY_BIN=/tmp/hst-imager-legacy/Hst.Imager.ConsoleApp.dll
  fi
fi

if [[ -z "$LEGACY_BIN" ]]; then
  echo "ERROR: legacy backend not found. Set HST_IMAGER_LEGACY_BIN."
  exit 1
fi

echo "Building go binary at $GO_BIN"
go build -o "$GO_BIN" .

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

PASS=0
FAIL=0
SKIP=0

DATA_ROOT="$ROOT_DIR/../Hst.Imager.Core.Tests/TestData"
LHA_AMIGA="$DATA_ROOT/Lha/amiga.lha"
LZX_AMIGA="$DATA_ROOT/Lzx/amiga.lzx"
RAR_IMG="$DATA_ROOT/compressed-images/1gb.img.rar"
GZ_IMG="$DATA_ROOT/compressed-images/1gb.img.gz"
XZ_IMG="$DATA_ROOT/compressed-images/1gb.img.xz"
XZ_SMALL="$DATA_ROOT/Xz/test.txt.xz"

normalize_text() {
  local in_file="$1"
  local out_file="$2"
  sed -e 's/\r$//' "$in_file" >"$out_file"
}

run_mode() {
  local mode="$1"
  local out_file="$2"
  local err_file="$3"
  local code_file="$4"
  shift 4

  set +e
  HST_IMAGER_LEGACY_MODE="$mode" \
    HST_IMAGER_LEGACY_BIN="$LEGACY_BIN" \
    "$GO_BIN" "$@" >"$out_file" 2>"$err_file"
  local rc=$?
  set -e
  echo "$rc" >"$code_file"
}

pass_case() {
  PASS=$((PASS + 1))
  echo "PASS: $1"
}

fail_case() {
  FAIL=$((FAIL + 1))
  echo "FAIL: $1"
  echo "  $2"
}

skip_case() {
  SKIP=$((SKIP + 1))
  echo "SKIP: $1"
  echo "  $2"
}

compare_output_case() {
  local name="$1"
  shift

  local off_out="$TMP_DIR/${name}.off.out"
  local off_err="$TMP_DIR/${name}.off.err"
  local off_rc="$TMP_DIR/${name}.off.rc"
  local force_out="$TMP_DIR/${name}.force.out"
  local force_err="$TMP_DIR/${name}.force.err"
  local force_rc="$TMP_DIR/${name}.force.rc"

  run_mode off "$off_out" "$off_err" "$off_rc" "$@"
  run_mode force "$force_out" "$force_err" "$force_rc" "$@"

  if [[ "$(cat "$force_rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$force_out" "$force_err"; then
    skip_case "$name" "legacy backend does not expose this command in the published baseline"
    return
  fi

  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.rc.diff"; then
    fail_case "$name" "exit code mismatch (see $TMP_DIR/${name}.rc.diff)"
    return
  fi

  normalize_text "$off_out" "$off_out.norm"
  normalize_text "$force_out" "$force_out.norm"
  normalize_text "$off_err" "$off_err.norm"
  normalize_text "$force_err" "$force_err.norm"

  if ! diff -u "$off_out.norm" "$force_out.norm" >"$TMP_DIR/${name}.stdout.diff"; then
    fail_case "$name" "stdout mismatch (see $TMP_DIR/${name}.stdout.diff)"
    return
  fi
  if ! diff -u "$off_err.norm" "$force_err.norm" >"$TMP_DIR/${name}.stderr.diff"; then
    fail_case "$name" "stderr mismatch (see $TMP_DIR/${name}.stderr.diff)"
    return
  fi

  pass_case "$name"
}

compare_fs_dir_json_case() {
  local name="$1"
  local source="$2"
  local off_out="$TMP_DIR/${name}.off.out"
  local off_err="$TMP_DIR/${name}.off.err"
  local off_rc="$TMP_DIR/${name}.off.rc"
  local force_out="$TMP_DIR/${name}.force.out"
  local force_err="$TMP_DIR/${name}.force.err"
  local force_rc="$TMP_DIR/${name}.force.rc"

  run_mode off "$off_out" "$off_err" "$off_rc" --format json fs dir "$source"
  run_mode force "$force_out" "$force_err" "$force_rc" --format json fs dir "$source"

  if [[ "$(cat "$force_rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$force_out" "$force_err"; then
    skip_case "$name" "legacy backend does not expose this command in the published baseline"
    return
  fi
  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.rc.diff"; then
    fail_case "$name" "exit code mismatch (see $TMP_DIR/${name}.rc.diff)"
    return
  fi

  if ! jq -S '{entries: (.entries // [] | map({name,size,type:(.type|tostring),attributes:(.attributes//"")}) | sort_by(.name,.size,.type))}' "$off_out" >"$off_out.norm"; then
    fail_case "$name" "go output is not valid json"
    return
  fi
  if ! jq -S '{entries: (.entries // [] | map({name,size,type:(.type|tostring),attributes:(.attributes//"")}) | sort_by(.name,.size,.type))}' "$force_out" >"$force_out.norm"; then
    fail_case "$name" "legacy output is not valid json"
    return
  fi

  if ! diff -u "$off_out.norm" "$force_out.norm" >"$TMP_DIR/${name}.json.diff"; then
    fail_case "$name" "json semantic mismatch (see $TMP_DIR/${name}.json.diff)"
    return
  fi

  pass_case "$name"
}

snapshot_tree() {
  local dir="$1"
  local out_file="$2"
  : >"$out_file"
  if [[ ! -d "$dir" ]]; then
    return
  fi
  (
    cd "$dir"
    find . -mindepth 1 -print0 | LC_ALL=C sort -z | while IFS= read -r -d '' path; do
      rel="${path#./}"
      if [[ -d "$path" ]]; then
        printf "D %s\n" "$rel"
      elif [[ -f "$path" ]]; then
        size="$(wc -c <"$path" | tr -d '[:space:]')"
        sha="$(shasum -a 256 "$path" | awk '{print $1}')"
        printf "F %s %s %s\n" "$rel" "$size" "$sha"
      else
        printf "O %s\n" "$rel"
      fi
    done
  ) >>"$out_file"
}

compare_extract_case() {
  local name="$1"
  local source="$2"
  local off_dest="$TMP_DIR/${name}.off.dir"
  local force_dest="$TMP_DIR/${name}.force.dir"
  mkdir -p "$off_dest" "$force_dest"

  local off_out="$TMP_DIR/${name}.off.out"
  local off_err="$TMP_DIR/${name}.off.err"
  local off_rc="$TMP_DIR/${name}.off.rc"
  local force_out="$TMP_DIR/${name}.force.out"
  local force_err="$TMP_DIR/${name}.force.err"
  local force_rc="$TMP_DIR/${name}.force.rc"

  run_mode off "$off_out" "$off_err" "$off_rc" fs extract "$source" "$off_dest" --recursive
  run_mode force "$force_out" "$force_err" "$force_rc" fs extract "$source" "$force_dest" --recursive

  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.rc.diff"; then
    fail_case "$name" "exit code mismatch (see $TMP_DIR/${name}.rc.diff)"
    return
  fi

  snapshot_tree "$off_dest" "$TMP_DIR/${name}.off.tree"
  snapshot_tree "$force_dest" "$TMP_DIR/${name}.force.tree"
  if ! diff -u "$TMP_DIR/${name}.off.tree" "$TMP_DIR/${name}.force.tree" >"$TMP_DIR/${name}.tree.diff"; then
    fail_case "$name" "extracted tree mismatch (see $TMP_DIR/${name}.tree.diff)"
    return
  fi

  pass_case "$name"
}

compare_write_case() {
  local name="$1"
  local source="$2"
  local write_size="${3:-}"
  local off_file="$TMP_DIR/${name}.off.bin"
  local force_file="$TMP_DIR/${name}.force.bin"

  local off_out="$TMP_DIR/${name}.off.out"
  local off_err="$TMP_DIR/${name}.off.err"
  local off_rc="$TMP_DIR/${name}.off.rc"
  local force_out="$TMP_DIR/${name}.force.out"
  local force_err="$TMP_DIR/${name}.force.err"
  local force_rc="$TMP_DIR/${name}.force.rc"

  run_mode off "$TMP_DIR/${name}.seed.off.out" "$TMP_DIR/${name}.seed.off.err" "$TMP_DIR/${name}.seed.off.rc" blank "$off_file" 1MB
  run_mode force "$TMP_DIR/${name}.seed.force.out" "$TMP_DIR/${name}.seed.force.err" "$TMP_DIR/${name}.seed.force.rc" blank "$force_file" 1MB
  if [[ "$(cat "$TMP_DIR/${name}.seed.force.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.force.out" "$TMP_DIR/${name}.seed.force.err"; then
    skip_case "$name" "legacy backend does not expose blank command in the published baseline"
    return
  fi

  local write_args=(write "$source" "$off_file")
  if [[ -n "$write_size" ]]; then
    write_args+=(--size "$write_size")
  fi
  run_mode off "$off_out" "$off_err" "$off_rc" "${write_args[@]}"

  write_args=(write "$source" "$force_file")
  if [[ -n "$write_size" ]]; then
    write_args+=(--size "$write_size")
  fi
  run_mode force "$force_out" "$force_err" "$force_rc" "${write_args[@]}"

  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.rc.diff"; then
    fail_case "$name" "exit code mismatch (see $TMP_DIR/${name}.rc.diff)"
    return
  fi
  if [[ ! -f "$off_file" || ! -f "$force_file" ]]; then
    fail_case "$name" "missing output files"
    return
  fi

  local off_sha force_sha off_size force_size
  off_sha="$(shasum -a 256 "$off_file" | awk '{print $1}')"
  force_sha="$(shasum -a 256 "$force_file" | awk '{print $1}')"
  off_size="$(wc -c <"$off_file" | tr -d '[:space:]')"
  force_size="$(wc -c <"$force_file" | tr -d '[:space:]')"

  if [[ "$off_sha" != "$force_sha" || "$off_size" != "$force_size" ]]; then
    fail_case "$name" "output file hash/size mismatch"
    return
  fi

  pass_case "$name"
}

compare_compare_case() {
  local name="$1"
  local expected_rc="$2"
  local source_file="$3"
  local destination_file="$4"
  local compare_size="${5:-}"

  local off_out="$TMP_DIR/${name}.off.out"
  local off_err="$TMP_DIR/${name}.off.err"
  local off_rc="$TMP_DIR/${name}.off.rc"
  local force_out="$TMP_DIR/${name}.force.out"
  local force_err="$TMP_DIR/${name}.force.err"
  local force_rc="$TMP_DIR/${name}.force.rc"

  local compare_args=(compare "$source_file" "$destination_file")
  if [[ -n "$compare_size" ]]; then
    compare_args+=(--size "$compare_size")
  fi
  run_mode off "$off_out" "$off_err" "$off_rc" "${compare_args[@]}"
  run_mode force "$force_out" "$force_err" "$force_rc" "${compare_args[@]}"

  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.rc.diff"; then
    fail_case "$name" "exit code mismatch (see $TMP_DIR/${name}.rc.diff)"
    return
  fi
  if [[ "$(cat "$off_rc")" != "$expected_rc" ]]; then
    fail_case "$name" "unexpected exit code from go mode: got $(cat "$off_rc"), expected $expected_rc"
    return
  fi

  pass_case "$name"
}

echo "Running output parity checks"
compare_fs_dir_json_case "fs-dir-json-lha-amiga" "$LHA_AMIGA"
compare_fs_dir_json_case "fs-dir-json-lzx-amiga" "$LZX_AMIGA"
compare_fs_dir_json_case "fs-dir-json-rar-img" "$RAR_IMG"
compare_fs_dir_json_case "fs-dir-json-gz-img" "$GZ_IMG"
compare_fs_dir_json_case "fs-dir-json-xz-img" "$XZ_IMG"
compare_fs_dir_json_case "fs-dir-json-xz-text" "$XZ_SMALL"

echo "Running extraction parity checks"
compare_extract_case "extract-lha-amiga" "$LHA_AMIGA"
compare_extract_case "extract-lzx-amiga" "$LZX_AMIGA"

echo "Running write parity checks"
compare_write_case "write-xz-small" "$XZ_SMALL"
compare_write_case "write-xz-small-size5" "$XZ_SMALL" "5"
if [[ "$HEAVY" == "1" ]]; then
  compare_write_case "write-rar-img-heavy" "$RAR_IMG"
fi

echo "Running compare parity checks"
printf 'abcde12345' >"$TMP_DIR/compare-a.bin"
printf 'abcde12345' >"$TMP_DIR/compare-b-equal.bin"
printf 'abcXY12345' >"$TMP_DIR/compare-b-diff.bin"
compare_compare_case "compare-size5-identical" "0" "$TMP_DIR/compare-a.bin" "$TMP_DIR/compare-b-equal.bin" "5"
compare_compare_case "compare-size5-mismatch" "1" "$TMP_DIR/compare-a.bin" "$TMP_DIR/compare-b-diff.bin" "5"

echo "Summary: PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
if [[ "$FAIL" -ne 0 ]]; then
  echo "Detailed artifacts left in: $TMP_DIR"
  trap - EXIT
  exit 1
fi
