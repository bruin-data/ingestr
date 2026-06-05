#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/../config.sh"

SEED_SIZES="${BENCH_SEED_SIZES:-$BENCH_ROWS}"
echo "==> Seeding sizes: $SEED_SIZES"

mongo_count() {
    local collection="$1"
    docker exec "$MONGO_SRC_CONTAINER" mongosh --quiet \
        --eval "db.getSiblingDB('$MONGO_SRC_DB').getCollection('$collection').countDocuments()" \
        2>/dev/null || echo "0"
}

mongo_id_type() {
    local collection="$1"
    docker exec "$MONGO_SRC_CONTAINER" mongosh --quiet \
        --eval "const doc = db.getSiblingDB('$MONGO_SRC_DB').getCollection('$collection').findOne({}, {_id: 1}); doc ? typeof doc._id : ''" \
        2>/dev/null || echo ""
}

mongo_first_big_int() {
    local collection="$1"
    docker exec "$MONGO_SRC_CONTAINER" mongosh --quiet \
        --eval "const doc = db.getSiblingDB('$MONGO_SRC_DB').getCollection('$collection').findOne({}, {big_int: 1}); doc ? doc.big_int.toString() : ''" \
        2>/dev/null || echo ""
}

mongo_bson_marker() {
    local collection="$1"
    docker exec "$MONGO_SRC_CONTAINER" mongosh --quiet \
        --eval "const doc = db.getSiblingDB('$MONGO_SRC_DB').getCollection('$collection').aggregate([{ \$limit: 1 }, { \$project: { _id_type: { \$type: '\$_id' }, date_val_type: { \$type: '\$date_val' }, json_val_type: { \$type: '\$json_val' }, array_val_type: { \$type: '\$array_val' }, binary_val_type: { \$type: '\$binary_val' }, decimal128_val_type: { \$type: '\$decimal128_val' }, timestamp_val_type: { \$type: '\$timestamp_val' }, regex_val_type: { \$type: '\$regex_val' }, mixed_val_type: { \$type: '\$mixed_val' } } }]).toArray()[0]; doc ? print([doc._id_type, doc.date_val_type, doc.json_val_type, doc.array_val_type, doc.binary_val_type, doc.decimal128_val_type, doc.timestamp_val_type, doc.regex_val_type, doc.mixed_val_type].join('|')) : print('')" \
        2>/dev/null || echo ""
}

js_string() {
    local value="${1//\\/\\\\}"
    value="${value//\'/\\\'}"
    printf "'%s'" "$value"
}

for rows in $SEED_SIZES; do
    suffix="$(bench_size_suffix "$rows")"
    table_name="bench_data_${suffix}"
    bson_table_name="bench_data_bson_${suffix}"

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
    MONGO_JSON_PATH="$DUCKDB_DIR/seed_mongo_${suffix}.jsonl"

    pg_needs_seed=false
    existing=$(psql "$PG_SRC_URI" -t -A -c "SELECT count(*) FROM public.${table_name}" 2>/dev/null || echo "0")
    [[ "$existing" != "$rows" ]] && pg_needs_seed=true

    mysql_needs_seed=false
    existing=$(mysql --protocol=tcp -h "$MYSQL_SRC_HOST" -P "$MYSQL_SRC_PORT" -u root -proot_pass \
        -N -e "SELECT count(*) FROM ${table_name}" "$MYSQL_SRC_DB" 2>/dev/null || echo "0")
    [[ "$existing" != "$rows" ]] && mysql_needs_seed=true

    mongo_needs_seed=false
    existing=$(mongo_count "$table_name")
    id_type=$(mongo_id_type "$table_name")
    first_big_int=$(mongo_first_big_int "$table_name")
    expected_first_big_int=$((rows * 1000000))
    [[ "$existing" != "$rows" || "$id_type" != "string" || "$first_big_int" != "$expected_first_big_int" ]] && mongo_needs_seed=true

    mongo_bson_needs_seed=false
    existing=$(mongo_count "$bson_table_name")
    bson_marker=$(mongo_bson_marker "$bson_table_name")
    expected_bson_marker="objectId|date|missing|array|binData|decimal|timestamp|regex|missing"
    [[ "$existing" != "$rows" || "$bson_marker" != "$expected_bson_marker" ]] && mongo_bson_needs_seed=true

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

    # --- MongoDB (via JSONL from DuckDB) ---
    if [[ "$mongo_needs_seed" == "true" ]]; then
        echo "  MongoDB: exporting JSONL from DuckDB..."
        duckdb "$DUCKDB_SRC_PATH" -c "
COPY (
    SELECT
        'id_' || CAST(id AS VARCHAR) AS _id,
        id, small_str, medium_str, large_str, tiny_int,
        regular_int, big_int, float_val,
        decimal_val::DOUBLE AS decimal_val,
        bool_val,
        CAST(date_val AS VARCHAR) AS date_val,
        strftime(ts_val, '%Y-%m-%d %H:%M:%S.%g') AS ts_val,
        strftime(ts_tz_val, '%Y-%m-%d %H:%M:%S.%g') AS ts_tz_val,
        json_val, extra_text
    FROM ${table_name}
    ORDER BY id DESC
) TO '${MONGO_JSON_PATH}' (FORMAT json, ARRAY false);
"
        echo "  MongoDB: loading JSONL via mongoimport..."
        docker exec -i "$MONGO_SRC_CONTAINER" mongoimport \
            --db "$MONGO_SRC_DB" \
            --collection "$table_name" \
            --drop \
            --maintainInsertionOrder \
            --type json < "$MONGO_JSON_PATH" >/dev/null
        docker exec "$MONGO_SRC_CONTAINER" mongosh --quiet \
            --eval "db.getSiblingDB('$MONGO_SRC_DB').getCollection('$table_name').createIndex({id: 1})" >/dev/null
        rm -f "$MONGO_JSON_PATH"
        echo "  MongoDB: done ($(mongo_count "$table_name") rows)"
    else
        echo "  MongoDB: already seeded, skipping"
    fi

    # --- MongoDB BSON (native MongoDB types via mongosh) ---
    if [[ "$mongo_bson_needs_seed" == "true" ]]; then
        echo "  MongoDB BSON: seeding native BSON documents..."
        {
            printf "const seedDatabase = %s;\n" "$(js_string "$MONGO_SRC_DB")"
            printf "const seedCollection = %s;\n" "$(js_string "$bson_table_name")"
            printf "const seedRows = %d;\n" "$rows"
            printf "const seedBatchSize = %d;\n" 10000
            cat "$BENCH_DIR/scripts/seed_mongodb_bson.js"
        } | docker exec -i "$MONGO_SRC_CONTAINER" mongosh --quiet --file /dev/stdin
        echo "  MongoDB BSON: done ($(mongo_count "$bson_table_name") rows)"
    else
        echo "  MongoDB BSON: already seeded, skipping"
    fi
done

echo ""
echo "==> Seeding complete."
