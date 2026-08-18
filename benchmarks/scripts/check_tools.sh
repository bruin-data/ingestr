#!/usr/bin/env bash
set -euo pipefail

REQUIRED_TOOLS=("hyperfine" "docker" "psql" "duckdb")
OPTIONAL_TOOLS=("uv" "sling" "java")

errors=0
for tool in "${REQUIRED_TOOLS[@]}"; do
    if ! command -v "$tool" &>/dev/null; then
        echo "ERROR: Required tool '$tool' not found"
        errors=$((errors + 1))
    else
        echo "  OK: $tool"
    fi
done

if [ $errors -gt 0 ]; then
    echo ""
    echo "Install missing required tools and retry."
    exit 1
fi

echo ""
available_tools=("gong")
for tool in "${OPTIONAL_TOOLS[@]}"; do
    if command -v "$tool" &>/dev/null; then
        available_tools+=("$tool")
        echo "  OK: $tool (optional)"
    else
        echo "  SKIP: $tool not found (scenarios for this tool will be skipped)"
    fi
done

echo ""
echo "Tools for benchmarking: ${available_tools[*]}"
