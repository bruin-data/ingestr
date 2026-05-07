#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BENCH_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Default sizes; override with BENCH_ALL_SIZES env var
BENCH_ALL_SIZES="${BENCH_ALL_SIZES:-1000 100000 1000000 10000000}"

echo "==> Running benchmarks for all sizes: $BENCH_ALL_SIZES"

for rows in $BENCH_ALL_SIZES; do
    echo ""
    echo "############################################################"
    echo "# Benchmarking with $rows rows"
    echo "############################################################"
    uv run "$SCRIPT_DIR/runner.py" --rows "$rows" "$@"
done

echo ""
echo "==> All sizes benchmarked. Results in: $BENCH_DIR/results/"
