#!/usr/bin/env bash
set -euo pipefail

# Thin wrapper around runner.py --validate for backward compatibility.
#
# Usage:
#   bash benchmarks/scripts/validate.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
exec uv run "$SCRIPT_DIR/runner.py" --validate "$@"
