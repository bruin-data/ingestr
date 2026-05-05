#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/../config.sh"

echo "==> Stopping benchmark containers..."
docker compose -f "$BENCH_DIR/docker-compose.yml" down -v 2>/dev/null || true

echo "==> Removing DuckDB files..."
rm -rf "$DUCKDB_DIR"

echo "==> Teardown complete."
echo "    Results kept in: $RESULTS_DIR/"
echo "    To also delete results: rm -rf $RESULTS_DIR/*.json $RESULTS_DIR/*.md"
