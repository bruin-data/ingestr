#!/usr/bin/env bash
set -euo pipefail

# Thin wrapper around runner.py --validate for backward compatibility.
#
# Usage:
#   bash benchmarks/scripts/validate.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BENCH_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
exec uv run --project "$BENCH_DIR" --locked python "$SCRIPT_DIR/runner.py" --validate "$@"
