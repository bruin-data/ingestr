#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/../config.sh"

SEED_SIZES="${BENCH_SEED_SIZES:-$BENCH_ROWS}"
echo "==> Seeding sizes: $SEED_SIZES"

for rows in $SEED_SIZES; do
    suffix="$(bench_size_suffix "$rows")"
    table_name="bench_data_${suffix}"

    echo ""
    echo "====== $table_name ($rows rows) ======"

    # --- DuckDB (seed first, Postgres and MySQL use its CSV export) ---
    existing=$(duckdb "$DUCKDB_SRC_PATH" -noheader -csv -c "SELECT count(*) FROM ${table_name}" 2>/dev/null || echo "0")
    if [[ "$existing" == "$rows" ]]; then
        echo "  DuckDB: already seeded, skipping"
    else
        echo "  DuckDB: seeding..."
        sed "s/BENCH_TABLE_PLACEHOLDER/${table_name}/g; s/BENCH_ROWS_PLACEHOLDER/${rows}/g" \
            "$BENCH_DIR/sql/duckdb_seed.sql" | duckdb "$DUCKDB_SRC_PATH"
        echo "  DuckDB: done ($(duckdb "$DUCKDB_SRC_PATH" -noheader -csv -c "SELECT count(*) FROM ${table_name}") rows)"
    fi

    # --- Export CSV from DuckDB (used by both Postgres and MySQL) ---
    PG_CSV_PATH="$DUCKDB_DIR/seed_pg_${suffix}.csv"
    MYSQL_CSV_PATH="$DUCKDB_DIR/seed_mysql_${suffix}.csv"

    pg_needs_seed=false
    existing=$(psql "$PG_SRC_URI" -t -A -c "SELECT count(*) FROM public.${table_name}" 2>/dev/null || echo "0")
    [[ "$existing" != "$rows" ]] && pg_needs_seed=true

    mysql_needs_seed=false
    existing=$(mysql --protocol=tcp -h "$MYSQL_SRC_HOST" -P "$MYSQL_SRC_PORT" -u root -proot_pass \
        -N -e "SELECT count(*) FROM ${table_name}" "$MYSQL_SRC_DB" 2>/dev/null || echo "0")
    [[ "$existing" != "$rows" ]] && mysql_needs_seed=true

    # --- Postgres (via COPY from DuckDB CSV) ---
    if [[ "$pg_needs_seed" == "true" ]]; then
        echo "  Postgres: exporting CSV from DuckDB..."
        duckdb "$DUCKDB_SRC_PATH" -c "
COPY (
    SELECT
        id, small_str, medium_str, large_str, tiny_int,
        regular_int, big_int, float_val, decimal_val,
        bool_val,
        date_val, ts_val, ts_tz_val,
        json_val, extra_text
    FROM ${table_name}
) TO '${PG_CSV_PATH}' (HEADER FALSE, DELIMITER E'\t', NULL 'NULL', QUOTE '', ESCAPE '');
"
        echo "  Postgres: creating schema and loading via COPY..."
        sed "s/BENCH_TABLE_PLACEHOLDER/${table_name}/g" "$BENCH_DIR/sql/postgres_seed.sql" | psql "$PG_SRC_URI" -q
        psql "$PG_SRC_URI" -c "\copy public.${table_name} FROM '${PG_CSV_PATH}' WITH (FORMAT text, DELIMITER E'\t', NULL 'NULL')" -q
        psql "$PG_SRC_URI" -c "ANALYZE public.${table_name}" -q
        rm -f "$PG_CSV_PATH"
        echo "  Postgres: done ($(psql "$PG_SRC_URI" -t -A -c "SELECT count(*) FROM public.${table_name}") rows)"
    else
        echo "  Postgres: already seeded, skipping"
    fi

    # --- MySQL (via CSV from DuckDB) ---
    if [[ "$mysql_needs_seed" == "true" ]]; then
        echo "  MySQL: exporting CSV from DuckDB..."
        duckdb "$DUCKDB_SRC_PATH" -c "
COPY (
    SELECT
        id, small_str, medium_str, large_str, tiny_int,
        regular_int, big_int, float_val, decimal_val,
        CASE WHEN bool_val THEN 1 ELSE 0 END AS bool_val,
        date_val,
        strftime(ts_val, '%Y-%m-%d %H:%M:%S.%g') AS ts_val,
        strftime(ts_tz_val, '%Y-%m-%d %H:%M:%S.%g') AS ts_tz_val,
        json_val, extra_text
    FROM ${table_name}
) TO '${MYSQL_CSV_PATH}' (HEADER FALSE, DELIMITER '\t', NULL 'NULL', QUOTE '', ESCAPE '');
"
        echo "  MySQL: creating schema..."
        sed "s/BENCH_TABLE_PLACEHOLDER/${table_name}/g" "$BENCH_DIR/sql/mysql_schema.sql" \
            | mysql --protocol=tcp -h "$MYSQL_SRC_HOST" -P "$MYSQL_SRC_PORT" -u root -proot_pass "$MYSQL_SRC_DB"

        echo "  MySQL: loading CSV..."
        mysql --protocol=tcp -h "$MYSQL_SRC_HOST" -P "$MYSQL_SRC_PORT" -u root -proot_pass \
            --local-infile=1 "$MYSQL_SRC_DB" \
            -e "SET GLOBAL local_infile=1; LOAD DATA LOCAL INFILE '${MYSQL_CSV_PATH}' INTO TABLE ${table_name} FIELDS TERMINATED BY '\t' LINES TERMINATED BY '\n' (id, small_str, medium_str, large_str, tiny_int, regular_int, big_int, float_val, decimal_val, bool_val, date_val, ts_val, ts_tz_val, json_val, extra_text);"
        rm -f "$MYSQL_CSV_PATH"
        echo "  MySQL: done ($(mysql --protocol=tcp -h "$MYSQL_SRC_HOST" -P "$MYSQL_SRC_PORT" -u root -proot_pass -N -e "SELECT count(*) FROM ${table_name}" "$MYSQL_SRC_DB") rows)"
    else
        echo "  MySQL: already seeded, skipping"
    fi
done

echo ""
echo "==> Seeding complete."
