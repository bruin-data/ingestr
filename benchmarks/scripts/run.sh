#!/usr/bin/env bash
set -euo pipefail

# Thin wrapper around runner.py for backward compatibility.
# Supports all existing env vars: BENCH_ROWS, BENCH_RUNS, BENCH_WARMUP, BENCH_TOOLS
#
# Usage:
#   bash benchmarks/scripts/run.sh
#   BENCH_ROWS=1000 BENCH_RUNS=3 bash benchmarks/scripts/run.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
exec uv run "$SCRIPT_DIR/runner.py" "$@"
