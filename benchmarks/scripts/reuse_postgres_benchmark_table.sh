#!/usr/bin/env bash
set -euo pipefail

container="${BENCH_POSTGRES_CONTAINER:-bench-pg-dest}"
database="${BENCH_POSTGRES_DATABASE:-bench_dest}"
user="${BENCH_POSTGRES_USER:-bench_user}"
table="${BENCH_POSTGRES_TABLE:-public.bench_data}"

exists="$(docker exec "$container" psql -U "$user" -d "$database" -At -v ON_ERROR_STOP=1 -c "SELECT to_regclass('$table') IS NOT NULL")"
if [[ "$exists" != "t" ]]; then
	exit 0
fi

docker exec "$container" psql -U "$user" -d "$database" -v ON_ERROR_STOP=1 -c "
  ALTER TABLE $table SET (vacuum_truncate = false, autovacuum_enabled = false);
  DELETE FROM $table;
" >/dev/null
docker exec "$container" psql -U "$user" -d "$database" -v ON_ERROR_STOP=1 -c "
  VACUUM (ANALYZE FALSE, TRUNCATE FALSE) $table;
" >/dev/null
