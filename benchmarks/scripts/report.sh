#!/usr/bin/env bash
set -euo pipefail

# Thin wrapper around runner.py --report for backward compatibility.
#
# Usage:
#   bash benchmarks/scripts/report.sh                              # latest results
#   bash benchmarks/scripts/report.sh benchmarks/results/20260315_155056  # specific prefix

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BENCH_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -n "${1:-}" ]; then
    exec uv run --project "$BENCH_DIR" --locked python "$SCRIPT_DIR/runner.py" --report "$1"
else
    exec uv run --project "$BENCH_DIR" --locked python "$SCRIPT_DIR/runner.py" --report
fi
