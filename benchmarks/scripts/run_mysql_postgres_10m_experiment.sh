#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 EXPERIMENT_ID" >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/../.." && pwd)"
experiment_id="$1"
result_dir="$root/benchmarks/results/experiments/$experiment_id"
binary="${BENCH_BINARY:-$root/bin/ingestr}"
profile_script="$root/benchmarks/scripts/profile_mysql_postgres_10m.sh"
reuse_script="$root/benchmarks/scripts/reuse_postgres_benchmark_table.sh"
summary_script="$root/benchmarks/scripts/summarize_mysql_postgres_experiment.sh"

if [[ -e "$result_dir" ]]; then
  echo "result directory already exists: $result_dir" >&2
  exit 1
fi
mkdir -p "$result_dir"

{
  echo "experiment_id=$experiment_id"
  echo "timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "git_revision=$(git -C "$root" rev-parse HEAD)"
  echo "binary=$binary"
  echo "binary_sha256=$(shasum -a 256 "$binary" | awk '{print $1}')"
  echo "build_mode=${BENCH_BUILD_MODE:-unspecified}"
  echo "go_version=$(go version)"
  echo "docker_memory_bytes=$(docker info --format '{{.MemTotal}}')"
  echo "source_uri=${BENCH_SOURCE_URI:-mysql://bench_user:bench_pass@localhost:3307/bench_source}"
  echo "dest_uri=${BENCH_DEST_URI:-postgres://bench_user:bench_pass@localhost:5441/bench_dest?sslmode=disable}"
  echo "parallelism=${BENCH_PARALLELISM:-5}"
  echo "page_size=${BENCH_PAGE_SIZE:-25000}"
  echo "default_command=${BENCH_DEFAULT_COMMAND:-0}"
  echo "runs=${BENCH_RUNS:-5}"
  echo "destination_reuse=delete+vacuum_truncate_false"
  echo "gogc=${GOGC:-default}"
  echo "partition_by=${BENCH_PARTITION_BY:-}"
  echo "partition_interval=${BENCH_PARTITION_INTERVAL:-}"
  echo "postgres_image=$(docker inspect --format '{{.Config.Image}}' bench-pg-dest)"
  echo "postgres_image_id=$(docker inspect --format '{{.Image}}' bench-pg-dest)"
  echo "mysql_net_buffer_length=$(docker exec bench-mysql-source mysql -uroot -proot_pass -NBe 'SELECT @@GLOBAL.net_buffer_length' 2>/dev/null)"
  echo "host_uptime=$(uptime)"
} > "$result_dir/metadata.txt"

docker exec bench-mysql-source mysql -uroot -proot_pass -NBe "
  SHOW GLOBAL STATUS WHERE Variable_name IN (
    'Innodb_buffer_pool_pages_data',
    'Innodb_buffer_pool_pages_free',
    'Innodb_buffer_pool_read_requests',
    'Innodb_buffer_pool_reads'
  );
" > "$result_dir/mysql-before.tsv" 2>/dev/null

hyperfine \
  --warmup 1 \
  --runs "${BENCH_RUNS:-5}" \
  --setup "docker exec bench-pg-dest psql -U bench_user -d bench_dest -v ON_ERROR_STOP=1 -c 'DROP TABLE IF EXISTS public.bench_data' >/dev/null" \
  --prepare "$reuse_script" \
  --export-json "$result_dir/hyperfine.json" \
  "$profile_script '$result_dir'"

docker exec bench-mysql-source mysql -uroot -proot_pass -NBe "
  SHOW GLOBAL STATUS WHERE Variable_name IN (
    'Innodb_buffer_pool_pages_data',
    'Innodb_buffer_pool_pages_free',
    'Innodb_buffer_pool_read_requests',
    'Innodb_buffer_pool_reads'
  );
" > "$result_dir/mysql-after.tsv" 2>/dev/null

docker exec bench-pg-dest psql -U bench_user -d bench_dest -At -F $'\t' -c "
  SELECT 'fingerprint', count(*), min(id), max(id), sum(id), sum(regular_int), sum(big_int), sum(decimal_val), sum(bool_val::int)
  FROM public.bench_data;
  SELECT 'relation', c.relpersistence, pg_total_relation_size(c.oid),
         NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = c.oid AND contype = 'p'),
         NOT EXISTS (SELECT 1 FROM pg_index WHERE indrelid = c.oid)
  FROM pg_class AS c
  WHERE c.oid = 'public.bench_data'::regclass;
  SELECT 'database_stats', temp_files, temp_bytes, blk_read_time, blk_write_time
  FROM pg_stat_database
  WHERE datname = current_database();
  SELECT 'wal_stats', wal_records, wal_fpi, wal_bytes
  FROM pg_stat_wal;
" > "$result_dir/postgres-after.tsv"

"$reuse_script"
docker exec bench-pg-dest psql -U bench_user -d bench_dest -At -F $'\t' -c "
  SELECT 'reusable_relation', pg_relation_size('public.bench_data'), pg_total_relation_size('public.bench_data');
" > "$result_dir/postgres-reuse-before-profile.tsv"
INGESTR_CPUPROFILE="$result_dir/cpu.pprof" "$profile_script" "$result_dir"
go tool pprof -top "$binary" "$result_dir/cpu.pprof" > "$result_dir/cpu-top.txt"
go tool pprof -top -cum "$binary" "$result_dir/cpu.pprof" > "$result_dir/cpu-top-cumulative.txt"
"$summary_script" "$result_dir"

docker exec bench-pg-dest psql -U bench_user -d bench_dest -At -c "
  SELECT count(*), min(id), max(id), sum(id)
  FROM public.bench_data;
" > "$result_dir/profile-run-fingerprint.tsv"
