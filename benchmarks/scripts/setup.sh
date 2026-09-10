#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/../config.sh"

echo "==> Checking prerequisites..."
bash "$BENCH_DIR/scripts/check_tools.sh"

echo ""
echo "==> Starting benchmark containers..."
compose_args=()
if [[ "${BENCH_KAFKA:-0}" == "1" ]]; then
    compose_args+=(--profile kafka)
fi
docker compose -f "$BENCH_DIR/docker-compose.yml" "${compose_args[@]}" up -d --wait

echo ""
echo "==> Building gong..."
make -C "$PROJECT_ROOT" build

echo ""
echo "==> Creating directories..."
mkdir -p "$DUCKDB_DIR"
mkdir -p "$RESULTS_DIR"

echo ""
echo "==> Setup complete."
