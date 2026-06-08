#!/usr/bin/env bash
set -euo pipefail

# Thin wrapper around runner.py --validate for backward compatibility.
#
# Usage:
#   bash benchmarks/scripts/validate.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BENCH_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
exec uv run --no-project --python 3.13 --script "$SCRIPT_DIR/runner.py" --validate "$@"
