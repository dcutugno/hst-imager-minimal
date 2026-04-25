#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GO_EXE="$(go env GOEXE 2>/dev/null || true)"
GO_BIN="${GO_BIN:-$ROOT_DIR/.bin/hst-imager-go${GO_EXE}}"
LEGACY_BIN="${HST_IMAGER_LEGACY_BIN:-}"
HEAVY="${HST_PARITY_HEAVY:-0}"
DEEP_FS="${HST_PARITY_DEEP_FS:-0}"
RUN_ERROR_MATRIX="${HST_PARITY_ERROR_MATRIX:-1}"
REQUIRE_NO_SKIP="${HST_PARITY_REQUIRE_NO_SKIP:-0}"

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
mkdir -p "$(dirname "$GO_BIN")"
go build -o "$GO_BIN" .

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

PASS=0
FAIL=0
SKIP=0
CURRENT_FUZZ_CASE=""
CURRENT_FUZZ_SEED=""
CURRENT_FUZZ_TABLE=""

DATA_ROOT="$ROOT_DIR/../Hst.Imager.Core.Tests/TestData"
LHA_AMIGA="$DATA_ROOT/Lha/amiga.lha"
LHA_DIRS="$DATA_ROOT/Lha/dirs-files.lha"
LHA_SPECIAL="$DATA_ROOT/Lha/special_chars.lha"
LZX_AMIGA="$DATA_ROOT/Lzx/amiga.lzx"
LZX_DIRS="$DATA_ROOT/Lzx/dirs-files.lzx"
LZX_SPECIAL="$DATA_ROOT/Lzx/special_chars.lzx"
RAR_IMG="$DATA_ROOT/compressed-images/1gb.img.rar"
GZ_IMG="$DATA_ROOT/compressed-images/1gb.img.gz"
XZ_IMG="$DATA_ROOT/compressed-images/1gb.img.xz"
XZ_SMALL="$DATA_ROOT/Xz/test.txt.xz"
LZW_SMALL="$DATA_ROOT/Lzw/test.txt.Z"
ZIP_DIRS="$DATA_ROOT/Zip/dirs-files.zip"
ZIP_SPECIAL="$DATA_ROOT/Zip/special_chars.zip"
PFS3_AIO="$DATA_ROOT/Pfs3/pfs3aio"

hash_file() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$path" | awk '{print $NF}'
    return
  fi
  if command -v powershell >/dev/null 2>&1; then
    FILE_TO_HASH="$path" powershell -NoProfile -Command '$h=(Get-FileHash -Algorithm SHA256 -LiteralPath $env:FILE_TO_HASH).Hash; $h.ToLower()'
    return
  fi
  if command -v pwsh >/dev/null 2>&1; then
    FILE_TO_HASH="$path" pwsh -NoProfile -Command '$h=(Get-FileHash -Algorithm SHA256 -LiteralPath $env:FILE_TO_HASH).Hash; $h.ToLower()'
    return
  fi
  echo "ERROR: no SHA-256 tool available (shasum, sha256sum, openssl, or powershell required)" >&2
  exit 1
}

file_size_bytes() {
  wc -c <"$1" | tr -d '[:space:]'
}

is_windows_shell() {
  case "$(uname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')" in
    *mingw*|*msys*|*cygwin*) return 0 ;;
    *) return 1 ;;
  esac
}

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
  if is_fuzz_case_name "$1"; then
    write_fuzz_repro_script "$1"
  fi
}

skip_case() {
  SKIP=$((SKIP + 1))
  echo "SKIP: $1"
  echo "  $2"
}

is_fuzz_case_name() {
  [[ "$1" == fuzz-seq-* ]]
}

record_fuzz_case_metadata() {
  local case_name="$1"
  local seed="$2"
  local table="$3"
  printf '%s seed=%s table=%s\n' "$case_name" "$seed" "$table" >>"$TMP_DIR/fuzz-seeds.txt"
}

record_step_for_repro() {
  local case_name="$1"
  local mode="$2"
  shift 2
  local step_file="$TMP_DIR/${case_name}.${mode}.steps.sh"
  {
    printf 'HST_IMAGER_LEGACY_MODE=%q HST_IMAGER_LEGACY_BIN=%q %q' "$mode" "$LEGACY_BIN" "$GO_BIN"
    for arg in "$@"; do
      printf ' %q' "$arg"
    done
    printf '\n'
  } >>"$step_file"
}

write_fuzz_repro_script() {
  local case_name="$1"
  local off_steps="$TMP_DIR/${case_name}.off.steps.sh"
  local force_steps="$TMP_DIR/${case_name}.force.steps.sh"
  local repro_script="$TMP_DIR/repro-${case_name}.sh"
  if [[ ! -f "$off_steps" && ! -f "$force_steps" ]]; then
    return
  fi
  {
    echo "#!/usr/bin/env bash"
    echo "set -euo pipefail"
    printf 'cd %q\n' "$ROOT_DIR"
    printf 'echo %q\n' "Repro case: ${case_name} seed=${CURRENT_FUZZ_SEED} table=${CURRENT_FUZZ_TABLE}"
    if [[ -f "$off_steps" ]]; then
      echo "echo '--- OFF MODE STEPS ---'"
      cat "$off_steps"
    fi
    if [[ -f "$force_steps" ]]; then
      echo "echo '--- FORCE MODE STEPS ---'"
      cat "$force_steps"
    fi
    if [[ -f "$TMP_DIR/fuzz-seeds.txt" ]]; then
      printf 'echo %q\n' "Seed map: $TMP_DIR/fuzz-seeds.txt"
    fi
  } >"$repro_script"
  chmod +x "$repro_script"
  echo "  repro: $repro_script"
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
        size="$(file_size_bytes "$path")"
        sha="$(hash_file "$path")"
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
  off_sha="$(hash_file "$off_file")"
  force_sha="$(hash_file "$force_file")"
  off_size="$(file_size_bytes "$off_file")"
  force_size="$(file_size_bytes "$force_file")"

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

run_step_pair() {
  local case_name="$1"
  local step_name="$2"
  local off_media="$3"
  local force_media="$4"
  shift 4

  local off_args=()
  local force_args=()
  for arg in "$@"; do
    if [[ "$arg" == "__MEDIA__" ]]; then
      off_args+=("$off_media")
      force_args+=("$force_media")
    else
      off_args+=("$arg")
      force_args+=("$arg")
    fi
  done

  local off_out="$TMP_DIR/${case_name}.${step_name}.off.out"
  local off_err="$TMP_DIR/${case_name}.${step_name}.off.err"
  local off_rc="$TMP_DIR/${case_name}.${step_name}.off.rc"
  local force_out="$TMP_DIR/${case_name}.${step_name}.force.out"
  local force_err="$TMP_DIR/${case_name}.${step_name}.force.err"
  local force_rc="$TMP_DIR/${case_name}.${step_name}.force.rc"

  record_step_for_repro "$case_name" off "${off_args[@]}"
  record_step_for_repro "$case_name" force "${force_args[@]}"

  run_mode off "$off_out" "$off_err" "$off_rc" "${off_args[@]}"
  run_mode force "$force_out" "$force_err" "$force_rc" "${force_args[@]}"

  if [[ "$(cat "$force_rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$force_out" "$force_err"; then
    skip_case "$case_name" "legacy backend does not expose step '$step_name' in the published baseline"
    return 2
  fi
  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${case_name}.${step_name}.rc.diff"; then
    fail_case "$case_name" "exit code mismatch at step '$step_name' (see $TMP_DIR/${case_name}.${step_name}.rc.diff)"
    return 1
  fi
  return 0
}

compare_info_semantic_case() {
  local name="$1"
  local off_media="$2"
  local force_media="$3"
  local off_out="$TMP_DIR/${name}.off.info.out"
  local off_err="$TMP_DIR/${name}.off.info.err"
  local off_rc="$TMP_DIR/${name}.off.info.rc"
  local force_out="$TMP_DIR/${name}.force.info.out"
  local force_err="$TMP_DIR/${name}.force.info.err"
  local force_rc="$TMP_DIR/${name}.force.info.rc"

  run_mode off "$off_out" "$off_err" "$off_rc" --format json info "$off_media"
  run_mode off "$force_out" "$force_err" "$force_rc" --format json info "$force_media"
  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.info.rc.diff"; then
    fail_case "$name" "info exit code mismatch (see $TMP_DIR/${name}.info.rc.diff)"
    return
  fi

  if ! jq -S '{partitionTables:(.partitionTables // [] | sort), mbrParts:(if ((.partitionTables // []) | index("GPT")) != null then [] else (.mbr.parts // [] | map({index:(.index // 0), type:(.type // ""), start:(.start // 0), size:(.size // 0), name:(.name // ""), status:(.status // "")}) | sort_by(.index,.start,.size,.type,.name)) end), gptParts:(.gpt.parts // [] | map({index:(.index // 0), type:(.type // ""), start:(.start // 0), size:(.size // 0), name:(.name // ""), status:(.status // "")}) | sort_by(.index,.start,.size,.type,.name)), rdbParts:(.rdb.parts // [] | map({index:(.index // 0), type:(.type // ""), start:(.start // 0), size:(.size // 0), name:(.name // ""), status:(.status // "")}) | sort_by(.index,.start,.size,.type,.name))}' "$off_out" >"$off_out.norm"; then
    fail_case "$name" "go info output is not valid json"
    return
  fi
  if ! jq -S '{partitionTables:(.partitionTables // [] | sort), mbrParts:(if ((.partitionTables // []) | index("GPT")) != null then [] else (.mbr.parts // [] | map({index:(.index // 0), type:(.type // ""), start:(.start // 0), size:(.size // 0), name:(.name // ""), status:(.status // "")}) | sort_by(.index,.start,.size,.type,.name)) end), gptParts:(.gpt.parts // [] | map({index:(.index // 0), type:(.type // ""), start:(.start // 0), size:(.size // 0), name:(.name // ""), status:(.status // "")}) | sort_by(.index,.start,.size,.type,.name)), rdbParts:(.rdb.parts // [] | map({index:(.index // 0), type:(.type // ""), start:(.start // 0), size:(.size // 0), name:(.name // ""), status:(.status // "")}) | sort_by(.index,.start,.size,.type,.name))}' "$force_out" >"$force_out.norm"; then
    fail_case "$name" "force image info output is not valid json"
    return
  fi
  if ! diff -u "$off_out.norm" "$force_out.norm" >"$TMP_DIR/${name}.info.json.diff"; then
    fail_case "$name" "info semantic mismatch (see $TMP_DIR/${name}.info.json.diff)"
    return
  fi

  pass_case "$name"
}

compare_media_hash_case() {
  local name="$1"
  local off_media="$2"
  local force_media="$3"
  if [[ ! -f "$off_media" || ! -f "$force_media" ]]; then
    fail_case "$name" "missing media file(s) for hash compare"
    return
  fi
  local off_sha force_sha off_size force_size
  off_sha="$(hash_file "$off_media")"
  force_sha="$(hash_file "$force_media")"
  off_size="$(file_size_bytes "$off_media")"
  force_size="$(file_size_bytes "$force_media")"
  if [[ "$off_sha" != "$force_sha" || "$off_size" != "$force_size" ]]; then
    fail_case "$name" "media hash/size mismatch"
    return
  fi
  pass_case "$name-hash"
}

compare_error_semantic_case() {
  local name="$1"
  local expected_pattern="$2"
  shift 2

  local off_out="$TMP_DIR/${name}.off.out"
  local off_err="$TMP_DIR/${name}.off.err"
  local off_rc="$TMP_DIR/${name}.off.rc"
  local force_out="$TMP_DIR/${name}.force.out"
  local force_err="$TMP_DIR/${name}.force.err"
  local force_rc="$TMP_DIR/${name}.force.rc"

  run_mode off "$off_out" "$off_err" "$off_rc" "$@"
  run_mode force "$force_out" "$force_err" "$force_rc" "$@"

  if [[ "$(cat "$force_rc")" != "0" ]] && grep -Eq "Unrecognized command or argument" "$force_out" "$force_err"; then
    skip_case "$name" "legacy backend does not expose this command in the published baseline"
    return
  fi
  if ! diff -u "$off_rc" "$force_rc" >"$TMP_DIR/${name}.rc.diff"; then
    fail_case "$name" "exit code mismatch (see $TMP_DIR/${name}.rc.diff)"
    return
  fi
  if [[ "$(cat "$off_rc")" == "0" ]]; then
    fail_case "$name" "expected non-zero exit code for error path"
    return
  fi

  normalize_text "$off_out" "$off_out.norm"
  normalize_text "$off_err" "$off_err.norm"
  normalize_text "$force_out" "$force_out.norm"
  normalize_text "$force_err" "$force_err.norm"
  cat "$off_out.norm" "$off_err.norm" >"$off_out.all"
  cat "$force_out.norm" "$force_err.norm" >"$force_out.all"

  if [[ -n "$expected_pattern" ]]; then
    if ! grep -Eiq "$expected_pattern" "$off_out.all"; then
      fail_case "$name" "go error output missing expected pattern '$expected_pattern'"
      return
    fi
    if ! grep -Eiq "$expected_pattern" "$force_out.all"; then
      fail_case "$name" "legacy error output missing expected pattern '$expected_pattern'"
      return
    fi
  fi

  pass_case "$name"
}

run_error_matrix_cases() {
  compare_error_semantic_case "err-blank-missing-args" "usage" blank
  compare_error_semantic_case "err-info-missing-args" "usage" info
  compare_error_semantic_case "err-read-missing-args" "usage" read
  compare_error_semantic_case "err-write-missing-args" "usage" write
  compare_error_semantic_case "err-transfer-missing-args" "usage" transfer
  compare_error_semantic_case "err-compare-missing-args" "usage" compare
  compare_error_semantic_case "err-block-read-missing-args" "usage" block read
  compare_error_semantic_case "err-block-view-missing-args" "usage" block view
  compare_error_semantic_case "err-settings-update-missing-args" "usage" settings update
  compare_error_semantic_case "err-fs-dir-missing-args" "usage" fs dir
  compare_error_semantic_case "err-fs-copy-missing-args" "usage" fs copy
  compare_error_semantic_case "err-fs-extract-missing-args" "usage" fs extract
  compare_error_semantic_case "err-fs-mkdir-missing-args" "usage" fs mkdir
  compare_error_semantic_case "err-adf-create-missing-args" "usage" adf create
  compare_error_semantic_case "err-archive-list-missing-args" "usage" archive list
  compare_error_semantic_case "err-mbr-init-missing-args" "usage" mbr initialize
  compare_error_semantic_case "err-mbr-part-add-missing-args" "usage" mbr part add
  compare_error_semantic_case "err-mbr-part-delete-missing-args" "usage" mbr part delete
  compare_error_semantic_case "err-mbr-part-format-missing-args" "usage" mbr part format
  compare_error_semantic_case "err-mbr-part-export-missing-args" "usage" mbr part export
  compare_error_semantic_case "err-mbr-part-import-missing-args" "usage" mbr part import
  compare_error_semantic_case "err-mbr-part-clone-missing-args" "usage" mbr part clone
  compare_error_semantic_case "err-gpt-init-missing-args" "usage" gpt initialize
  compare_error_semantic_case "err-gpt-part-add-missing-args" "usage" gpt part add
  compare_error_semantic_case "err-gpt-part-delete-missing-args" "usage" gpt part delete
  compare_error_semantic_case "err-gpt-part-format-missing-args" "usage" gpt part format
  compare_error_semantic_case "err-rdb-init-missing-args" "usage" rdb initialize
  compare_error_semantic_case "err-rdb-fs-add-missing-args" "usage" rdb filesystem add
  compare_error_semantic_case "err-rdb-fs-update-missing-args" "usage" rdb filesystem update
  compare_error_semantic_case "err-rdb-fs-delete-missing-args" "usage" rdb filesystem delete
  compare_error_semantic_case "err-rdb-part-add-missing-args" "usage" rdb part add
  compare_error_semantic_case "err-rdb-part-update-missing-args" "usage" rdb part update
  compare_error_semantic_case "err-rdb-part-delete-missing-args" "usage" rdb part delete
  compare_error_semantic_case "err-rdb-part-copy-missing-args" "usage" rdb part copy
  compare_error_semantic_case "err-rdb-part-export-missing-args" "usage" rdb part export
  compare_error_semantic_case "err-rdb-part-import-missing-args" "usage" rdb part import
  compare_error_semantic_case "err-rdb-part-kill-missing-args" "usage" rdb part kill
  compare_error_semantic_case "err-rdb-part-move-missing-args" "usage" rdb part move
  compare_error_semantic_case "err-rdb-part-format-missing-args" "usage" rdb part format
  compare_error_semantic_case "err-rdb-backup-missing-args" "usage" rdb backup
  compare_error_semantic_case "err-rdb-restore-missing-args" "usage" rdb restore
  compare_error_semantic_case "err-script-missing-args" "usage" script
}

run_fuzz_workflow_cases() {
  local rounds="${HST_PARITY_FUZZ_ROUNDS:-0}"
  local base_seed="${HST_PARITY_FUZZ_SEED:-4242}"
  local strict_hash="${HST_PARITY_FUZZ_STRICT_HASH:-0}"
  local i seed name off_media force_media off_dir force_dir rc table add_count part_count idx size

  if [[ "$rounds" -le 0 ]]; then
    echo "Skipping randomized parity checks (HST_PARITY_FUZZ_ROUNDS=$rounds)"
    return
  fi

  for ((i = 1; i <= rounds; i++)); do
    seed=$((base_seed + i))
    RANDOM=$seed
    name="fuzz-seq-${i}"
    CURRENT_FUZZ_CASE="$name"
    CURRENT_FUZZ_SEED="$seed"
    CURRENT_FUZZ_TABLE="unknown"
    # Use identical media basenames for both modes to avoid path-derived RDB label/checksum noise.
    off_dir="$TMP_DIR/${name}.off"
    force_dir="$TMP_DIR/${name}.force"
    mkdir -p "$off_dir" "$force_dir"
    off_media="$off_dir/${name}.img"
    force_media="$force_dir/${name}.img"

    run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 64MB; rc=$?
    if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi

    table=$((RANDOM % 3))
    if [[ "$table" -eq 0 ]]; then
      CURRENT_FUZZ_TABLE="mbr"
      record_fuzz_case_metadata "$name" "$seed" "$CURRENT_FUZZ_TABLE"
      run_step_pair "$name" "mbr-init" "$off_media" "$force_media" mbr initialize "__MEDIA__"; rc=$?
      if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      add_count=$((RANDOM % 3 + 1))
      part_count=0
      for ((idx = 1; idx <= add_count; idx++)); do
        case $((RANDOM % 3)) in
          0) size="4MB" ;;
          1) size="8MB" ;;
          *) size="12MB" ;;
        esac
        run_step_pair "$name" "mbr-add-${idx}" "$off_media" "$force_media" mbr part add "__MEDIA__" fat32 "$size"; rc=$?
        if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
        part_count=$((part_count + 1))
      done
      if [[ "$part_count" -gt 1 && $((RANDOM % 2)) -eq 0 ]]; then
        idx=$((RANDOM % part_count + 1))
        run_step_pair "$name" "mbr-delete" "$off_media" "$force_media" mbr part delete "__MEDIA__" "$idx"; rc=$?
        if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      fi
    elif [[ "$table" -eq 1 ]]; then
      CURRENT_FUZZ_TABLE="gpt"
      record_fuzz_case_metadata "$name" "$seed" "$CURRENT_FUZZ_TABLE"
      run_step_pair "$name" "gpt-init" "$off_media" "$force_media" gpt initialize "__MEDIA__"; rc=$?
      if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      add_count=$((RANDOM % 2 + 1))
      part_count=0
      for ((idx = 1; idx <= add_count; idx++)); do
        if [[ $((RANDOM % 2)) -eq 0 ]]; then
          size="8MB"
        else
          size="16MB"
        fi
        run_step_pair "$name" "gpt-add-${idx}" "$off_media" "$force_media" gpt part add "__MEDIA__" ntfs "DATA${idx}" "$size"; rc=$?
        if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
        part_count=$((part_count + 1))
      done
      if [[ "$part_count" -gt 1 && $((RANDOM % 2)) -eq 0 ]]; then
        idx=$((RANDOM % part_count + 1))
        run_step_pair "$name" "gpt-delete" "$off_media" "$force_media" gpt part delete "__MEDIA__" "$idx"; rc=$?
        if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      fi
    else
      CURRENT_FUZZ_TABLE="rdb"
      record_fuzz_case_metadata "$name" "$seed" "$CURRENT_FUZZ_TABLE"
      run_step_pair "$name" "rdb-init" "$off_media" "$force_media" rdb initialize "__MEDIA__"; rc=$?
      if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      run_step_pair "$name" "rdb-fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
      if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      add_count=$((RANDOM % 2 + 1))
      part_count=0
      for ((idx = 1; idx <= add_count; idx++)); do
        size="8MB"
        run_step_pair "$name" "rdb-add-${idx}" "$off_media" "$force_media" rdb part add "__MEDIA__" "DH${idx}" PDS3 "$size"; rc=$?
        if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
        part_count=$((part_count + 1))
      done
      if [[ "$part_count" -gt 1 && $((RANDOM % 2)) -eq 0 ]]; then
        idx=$((RANDOM % part_count + 1))
        run_step_pair "$name" "rdb-delete" "$off_media" "$force_media" rdb part delete "__MEDIA__" "$idx"; rc=$?
        if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
      fi
    fi

    compare_info_semantic_case "$name" "$off_media" "$force_media"
    # Optional: strict raw-image hash check for fuzzed workflows.
    # Disabled by default because semantic parity can still hold while low-level container bytes differ.
    if [[ "$strict_hash" == "1" && "$table" -ne 1 ]]; then
      compare_media_hash_case "$name" "$off_media" "$force_media"
    fi
  done
  CURRENT_FUZZ_CASE=""
  CURRENT_FUZZ_SEED=""
  CURRENT_FUZZ_TABLE=""
}

run_partition_workflow_cases() {
  local name
  local off_media
  local force_media
  local seed_media
  local rc

  name="partition-mbr-basic"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 32MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" mbr initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" mbr part add "__MEDIA__" fat32 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-mbr-delete"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 32MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" mbr initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add-1" "$off_media" "$force_media" mbr part add "__MEDIA__" fat32 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add-2" "$off_media" "$force_media" mbr part add "__MEDIA__" fat16 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-delete-1" "$off_media" "$force_media" mbr part delete "__MEDIA__" 1; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-mbr-format-fat32-small"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 32MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" mbr initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" mbr part add "__MEDIA__" fat32 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-format" "$off_media" "$force_media" mbr part format "__MEDIA__" 1 PC; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-gpt-basic"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 32MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" gpt initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" gpt part add "__MEDIA__" ntfs DATA 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-gpt-delete"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 32MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" gpt initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add-1" "$off_media" "$force_media" gpt part add "__MEDIA__" ntfs DATA1 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add-2" "$off_media" "$force_media" gpt part add "__MEDIA__" ntfs DATA2 4MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-delete-1" "$off_media" "$force_media" gpt part delete "__MEDIA__" 1; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-gpt-format-ntfs"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 64MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" gpt initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" gpt part add "__MEDIA__" ntfs DATA 32MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-format" "$off_media" "$force_media" gpt part format "__MEDIA__" 1 ntfs VOL; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-rdb-init"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  run_step_pair "$name" "blank" "$off_media" "$force_media" blank "__MEDIA__" 64MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "initialize" "$off_media" "$force_media" rdb initialize "__MEDIA__"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"

  name="partition-rdb-fsadd-partadd"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"

  name="partition-rdb-fsupdate"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "fs-update" "$off_media" "$force_media" rdb filesystem update "__MEDIA__" 1 --dos-type PDS2 --name PFS3AIO; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"

  name="partition-rdb-fsupdate-path"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  fs_payload="$TMP_DIR/${name}.payload.bin"
  printf 'ABCDEF1234567890' >"$fs_payload"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "fs-update-path" "$off_media" "$force_media" rdb filesystem update "__MEDIA__" 1 --path "$fs_payload"; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"

  name="partition-rdb-partupdate-move"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-update" "$off_media" "$force_media" rdb part update "__MEDIA__" 1 --name SYS --no-mount true; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-move" "$off_media" "$force_media" rdb part move "__MEDIA__" 1 2; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"

  name="partition-rdb-part-kill"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-kill" "$off_media" "$force_media" rdb part kill "__MEDIA__" 1 00000000; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"

  name="partition-rdb-part-import-copy"
  seed_src="$TMP_DIR/${name}.seed-src.img"
  seed_dst="$TMP_DIR/${name}.seed-dst.img"
  off_src="$TMP_DIR/${name}.off-src.img"
  force_src="$TMP_DIR/${name}.force-src.img"
  off_dst="$TMP_DIR/${name}.off-dst.img"
  force_dst="$TMP_DIR/${name}.force-dst.img"
  payload="$TMP_DIR/${name}.payload.bin"
  printf 'ABCDEFGH' >"$payload"
  run_mode force "$TMP_DIR/${name}.seed.blank-src.out" "$TMP_DIR/${name}.seed.blank-src.err" "$TMP_DIR/${name}.seed.blank-src.rc" blank "$seed_src" 64MB
  run_mode force "$TMP_DIR/${name}.seed.blank-dst.out" "$TMP_DIR/${name}.seed.blank-dst.err" "$TMP_DIR/${name}.seed.blank-dst.rc" blank "$seed_dst" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init-src.out" "$TMP_DIR/${name}.seed.init-src.err" "$TMP_DIR/${name}.seed.init-src.rc" rdb initialize "$seed_src"
  run_mode force "$TMP_DIR/${name}.seed.init-dst.out" "$TMP_DIR/${name}.seed.init-dst.err" "$TMP_DIR/${name}.seed.init-dst.rc" rdb initialize "$seed_dst"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init-src.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init-src.out" "$TMP_DIR/${name}.seed.init-src.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  run_mode force "$TMP_DIR/${name}.seed.fs-src.out" "$TMP_DIR/${name}.seed.fs-src.err" "$TMP_DIR/${name}.seed.fs-src.rc" rdb filesystem add "$seed_src" "$PFS3_AIO" PDS3
  run_mode force "$TMP_DIR/${name}.seed.fs-dst.out" "$TMP_DIR/${name}.seed.fs-dst.err" "$TMP_DIR/${name}.seed.fs-dst.rc" rdb filesystem add "$seed_dst" "$PFS3_AIO" PDS3
  cp "$seed_src" "$off_src"
  cp "$seed_src" "$force_src"
  cp "$seed_dst" "$off_dst"
  cp "$seed_dst" "$force_dst"

  run_mode off "$TMP_DIR/${name}.import.off.out" "$TMP_DIR/${name}.import.off.err" "$TMP_DIR/${name}.import.off.rc" rdb part import "$payload" "$off_src" DH0 PDS3
  run_mode force "$TMP_DIR/${name}.import.force.out" "$TMP_DIR/${name}.import.force.err" "$TMP_DIR/${name}.import.force.rc" rdb part import "$payload" "$force_src" DH0 PDS3
  if [[ "$(cat "$TMP_DIR/${name}.import.force.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.import.force.out" "$TMP_DIR/${name}.import.force.err"; then
    skip_case "$name" "legacy backend does not expose rdb part import in the published baseline"
    return
  fi
  if ! diff -u "$TMP_DIR/${name}.import.off.rc" "$TMP_DIR/${name}.import.force.rc" >"$TMP_DIR/${name}.import.rc.diff"; then
    fail_case "$name" "exit code mismatch at step 'import' (see $TMP_DIR/${name}.import.rc.diff)"
    return
  fi

  run_mode off "$TMP_DIR/${name}.copy.off.out" "$TMP_DIR/${name}.copy.off.err" "$TMP_DIR/${name}.copy.off.rc" rdb part copy "$off_src" 1 "$off_dst"
  run_mode force "$TMP_DIR/${name}.copy.force.out" "$TMP_DIR/${name}.copy.force.err" "$TMP_DIR/${name}.copy.force.rc" rdb part copy "$force_src" 1 "$force_dst"
  if [[ "$(cat "$TMP_DIR/${name}.copy.force.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.copy.force.out" "$TMP_DIR/${name}.copy.force.err"; then
    skip_case "$name" "legacy backend does not expose rdb part copy in the published baseline"
    return
  fi
  if ! diff -u "$TMP_DIR/${name}.copy.off.rc" "$TMP_DIR/${name}.copy.force.rc" >"$TMP_DIR/${name}.copy.rc.diff"; then
    fail_case "$name" "exit code mismatch at step 'copy' (see $TMP_DIR/${name}.copy.rc.diff)"
    return
  fi

  compare_info_semantic_case "${name}-src" "$off_src" "$force_src"
  compare_media_hash_case "${name}-src" "$off_src" "$force_src"
  compare_info_semantic_case "${name}-dst" "$off_dst" "$force_dst"
  compare_media_hash_case "${name}-dst" "$off_dst" "$force_dst"

  name="partition-rdb-part-delete"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-delete" "$off_media" "$force_media" rdb part delete "__MEDIA__" 1; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"

  name="partition-rdb-fs-delete-after-part-delete"
  off_media="$TMP_DIR/${name}.off.img"
  force_media="$TMP_DIR/${name}.force.img"
  seed_media="$TMP_DIR/${name}.seed.img"
  run_mode force "$TMP_DIR/${name}.seed.blank.out" "$TMP_DIR/${name}.seed.blank.err" "$TMP_DIR/${name}.seed.blank.rc" blank "$seed_media" 64MB
  run_mode force "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err" "$TMP_DIR/${name}.seed.init.rc" rdb initialize "$seed_media"
  if [[ "$(cat "$TMP_DIR/${name}.seed.init.rc")" != "0" ]] && grep -Eq "Unrecognized command or argument|Show help and usage information" "$TMP_DIR/${name}.seed.init.out" "$TMP_DIR/${name}.seed.init.err"; then
    skip_case "$name" "legacy backend does not expose rdb initialize in the published baseline"
    return
  fi
  cp "$seed_media" "$off_media"
  cp "$seed_media" "$force_media"
  run_step_pair "$name" "fs-add" "$off_media" "$force_media" rdb filesystem add "__MEDIA__" "$PFS3_AIO" PDS3; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-add" "$off_media" "$force_media" rdb part add "__MEDIA__" DH0 PDS3 8MB; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "part-delete" "$off_media" "$force_media" rdb part delete "__MEDIA__" 1; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  run_step_pair "$name" "fs-delete" "$off_media" "$force_media" rdb filesystem delete "__MEDIA__" 1; rc=$?
  if [[ "$rc" -eq 2 ]]; then return; elif [[ "$rc" -ne 0 ]]; then return; fi
  compare_info_semantic_case "$name" "$off_media" "$force_media"
  compare_media_hash_case "$name" "$off_media" "$force_media"
}

echo "Running output parity checks"
compare_fs_dir_json_case "fs-dir-json-lha-amiga" "$LHA_AMIGA"
compare_fs_dir_json_case "fs-dir-json-lzx-amiga" "$LZX_AMIGA"
compare_fs_dir_json_case "fs-dir-json-rar-img" "$RAR_IMG"
compare_fs_dir_json_case "fs-dir-json-gz-img" "$GZ_IMG"
compare_fs_dir_json_case "fs-dir-json-xz-img" "$XZ_IMG"
compare_fs_dir_json_case "fs-dir-json-xz-text" "$XZ_SMALL"
compare_fs_dir_json_case "fs-dir-json-lzw-text" "$LZW_SMALL"
if [[ "$DEEP_FS" == "1" ]]; then
  compare_fs_dir_json_case "fs-dir-json-lha-dirs" "$LHA_DIRS"
  compare_fs_dir_json_case "fs-dir-json-lha-special" "$LHA_SPECIAL"
  compare_fs_dir_json_case "fs-dir-json-lzx-dirs" "$LZX_DIRS"
  compare_fs_dir_json_case "fs-dir-json-lzx-special" "$LZX_SPECIAL"
  compare_fs_dir_json_case "fs-dir-json-zip-dirs" "$ZIP_DIRS"
  compare_fs_dir_json_case "fs-dir-json-zip-special" "$ZIP_SPECIAL"
fi

echo "Running extraction parity checks"
compare_extract_case "extract-lha-amiga" "$LHA_AMIGA"
compare_extract_case "extract-lzx-amiga" "$LZX_AMIGA"
compare_extract_case "extract-lzw-text" "$LZW_SMALL"
if [[ "$DEEP_FS" == "1" ]]; then
  compare_extract_case "extract-lha-dirs" "$LHA_DIRS"
  if is_windows_shell; then
    skip_case "extract-lha-special" "special-character fixture is not portable on Windows filesystem"
  else
    compare_extract_case "extract-lha-special" "$LHA_SPECIAL"
  fi
  compare_extract_case "extract-lzx-dirs" "$LZX_DIRS"
  if is_windows_shell; then
    skip_case "extract-lzx-special" "special-character fixture is not portable on Windows filesystem"
  else
    compare_extract_case "extract-lzx-special" "$LZX_SPECIAL"
  fi
  compare_extract_case "extract-zip-dirs" "$ZIP_DIRS"
  if is_windows_shell; then
    skip_case "extract-zip-special" "special-character fixture is not portable on Windows filesystem"
  else
    compare_extract_case "extract-zip-special" "$ZIP_SPECIAL"
  fi
fi

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

echo "Running partition workflow parity checks"
run_partition_workflow_cases

if [[ "$RUN_ERROR_MATRIX" == "1" ]]; then
  echo "Running error-path parity matrix"
  run_error_matrix_cases
fi

echo "Running randomized parity workflows"
run_fuzz_workflow_cases

echo "Summary: PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
if [[ "$FAIL" -ne 0 ]]; then
  echo "Detailed artifacts left in: $TMP_DIR"
  trap - EXIT
  exit 1
fi
if [[ "$REQUIRE_NO_SKIP" == "1" && "$SKIP" -ne 0 ]]; then
  echo "FAIL: expected zero skipped parity cases but got SKIP=$SKIP"
  echo "Detailed artifacts left in: $TMP_DIR"
  trap - EXIT
  exit 1
fi
