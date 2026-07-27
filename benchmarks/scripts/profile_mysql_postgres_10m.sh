#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 RESULT_DIR" >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/../.." && pwd)"
result_dir="$1"
binary="${BENCH_BINARY:-$root/bin/ingestr}"
run_number="$(find "$result_dir" -maxdepth 1 -name 'run-*.log' | wc -l | tr -d ' ')"
run_number="$((run_number + 1))"
sample="$(printf '%s/run-%03d.log' "$result_dir" "$run_number")"
parallelism="${BENCH_PARALLELISM:-5}"
page_size="${BENCH_PAGE_SIZE:-25000}"
source_uri="${BENCH_SOURCE_URI:-mysql://bench_user:bench_pass@localhost:3307/bench_source}"
dest_uri="${BENCH_DEST_URI:-postgres://bench_user:bench_pass@localhost:5441/bench_dest?sslmode=disable}"
partition_args=()
extract_args=(
	"--extract-parallelism=$parallelism"
	"--page-size=$page_size"
)
if [[ "${BENCH_DEFAULT_COMMAND:-0}" == "1" ]]; then
	extract_args=()
fi
if [[ "${BENCH_MYSQL_COMPRESS:-0}" == "1" ]]; then
  source_uri+='?compress=true'
fi
if [[ "${BENCH_DEFAULT_COMMAND:-0}" != "1" && -n "${BENCH_PARTITION_BY:-}" ]]; then
	partition_args+=(
		"--extract-partition-by=$BENCH_PARTITION_BY"
		"--extract-partition-interval=${BENCH_PARTITION_INTERVAL:-auto}"
	)
	if [[ -n "${BENCH_INCREMENTAL_KEY:-}" ]]; then
		partition_args+=(
			"--incremental-key=$BENCH_INCREMENTAL_KEY"
			"--interval-start=${BENCH_INTERVAL_START:-2020-01-01T00:00:00Z}"
			"--interval-end=${BENCH_INTERVAL_END:-2020-05-01T00:00:00Z}"
		)
	fi
fi
set +e
INGESTR_DISABLE_TELEMETRY=true \
DISABLE_TELEMETRY=true \
PROGRESS=log \
/usr/bin/time -l "$binary" ingest \
  --source-uri="$source_uri" \
  --dest-uri="$dest_uri" \
  --source-table='bench_source.bench_data_10m' \
  --dest-table='public.bench_data' \
  --incremental-strategy='append' \
  "${extract_args[@]}" \
  "${partition_args[@]}" \
  --yes >"$sample" 2>&1
rc=$?
set -e

if [[ $rc -ne 0 ]]; then
  cat "$sample" >&2
  exit "$rc"
fi

awk '/maximum resident set size/ {print $1}' "$sample" >> "$result_dir/rss-bytes.txt"
awk -F'[│]' '/Peak Memory/ {value=$3; gsub(/[^0-9.]/, "", value); if (value != "") print value}' "$sample" >> "$result_dir/cli-peak-mib.txt"
awk -F'[│]' '/Duration/ {value=$3; gsub(/[^0-9.]/, "", value); if (value != "") print value}' "$sample" >> "$result_dir/ingestr-duration-seconds.txt"
