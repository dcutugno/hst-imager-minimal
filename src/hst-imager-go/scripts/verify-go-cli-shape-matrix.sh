#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "[1/2] Running CLI shape parity matrix tests"
go test -count=1 -run 'TestCliShapeParityWithDotNetFactories|TestCliAliasResolutionMatrixFromDotNetAliases|TestCliOptionAssignmentMatrixFromDotNetOptions' ./...

echo "[2/2] CLI shape parity matrix tests passed"
