#!/usr/bin/env bash
# benchmarks/config.sh - Central benchmark configuration
# All values overridable via environment variables.

# --- Tunables ---
BENCH_ROWS="${BENCH_ROWS:-10000000}"
BENCH_RUNS="${BENCH_RUNS:-5}"
BENCH_WARMUP="${BENCH_WARMUP:-1}"

# Map row count to size suffix for table names
bench_size_suffix() {
    case "$1" in
        1000)       echo "1k" ;;
        100000)     echo "100k" ;;
        1000000)    echo "1m" ;;
        10000000)   echo "10m" ;;
        100000000)  echo "100m" ;;
        1000000000) echo "1b" ;;
        *)          echo "$1" ;;
    esac
}

BENCH_SIZE="$(bench_size_suffix "$BENCH_ROWS")"
BENCH_TABLE="bench_data_${BENCH_SIZE}"
BENCH_DEST_TABLE="bench_data"

# All supported sizes for seeding
BENCH_ALL_SIZES="${BENCH_ALL_SIZES:-1000 100000 1000000 10000000}"

# --- Directories ---
BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$BENCH_DIR/.." && pwd)"
RESULTS_DIR="$BENCH_DIR/results"
DUCKDB_DIR="$BENCH_DIR/duckdb_files"

# --- Postgres Source ---
PG_SRC_HOST="localhost"
PG_SRC_PORT="5440"
PG_SRC_USER="bench_user"
PG_SRC_PASS="bench_pass"
PG_SRC_DB="bench_source"
PG_SRC_URI="postgres://${PG_SRC_USER}:${PG_SRC_PASS}@${PG_SRC_HOST}:${PG_SRC_PORT}/${PG_SRC_DB}?sslmode=disable"

# --- Postgres Destination ---
PG_DST_HOST="localhost"
PG_DST_PORT="5441"
PG_DST_USER="bench_user"
PG_DST_PASS="bench_pass"
PG_DST_DB="bench_dest"
PG_DST_URI="postgres://${PG_DST_USER}:${PG_DST_PASS}@${PG_DST_HOST}:${PG_DST_PORT}/${PG_DST_DB}?sslmode=disable"

# --- SQL Server Destination ---
MSSQL_DST_HOST="localhost"
MSSQL_DST_PORT="1434"
MSSQL_DST_USER="sa"
MSSQL_DST_PASS="TestPassword123!"
MSSQL_DST_PASS_ENC="TestPassword123%21"
MSSQL_DST_DB="master"
MSSQL_DST_URI="mssql://${MSSQL_DST_USER}:${MSSQL_DST_PASS_ENC}@${MSSQL_DST_HOST}:${MSSQL_DST_PORT}/${MSSQL_DST_DB}?encrypt=disable"

# --- MySQL Source ---
MYSQL_SRC_HOST="localhost"
MYSQL_SRC_PORT="3307"
MYSQL_SRC_USER="bench_user"
MYSQL_SRC_PASS="bench_pass"
MYSQL_SRC_DB="bench_source"
MYSQL_SRC_URI="mysql://${MYSQL_SRC_USER}:${MYSQL_SRC_PASS}@${MYSQL_SRC_HOST}:${MYSQL_SRC_PORT}/${MYSQL_SRC_DB}"

# --- MongoDB Source ---
MONGO_SRC_HOST="localhost"
MONGO_SRC_PORT="27018"
MONGO_SRC_DB="bench_source"
MONGO_SRC_CONTAINER="bench-mongo-source"
MONGO_SRC_URI="mongodb://${MONGO_SRC_HOST}:${MONGO_SRC_PORT}/?directConnection=true&replicaSet=rs0"

# --- DuckDB ---
DUCKDB_SRC_PATH="$DUCKDB_DIR/bench_source.duckdb"
DUCKDB_DST_PATH="$DUCKDB_DIR/bench_dest.duckdb"
DUCKDB_SRC_URI="duckdb:///${DUCKDB_SRC_PATH}"
DUCKDB_DST_URI="duckdb:///${DUCKDB_DST_PATH}"

# --- sling env var names (sling references connections by env var name) ---
SLING_PG_SRC_ENV="POSTGRES_BENCH_SRC"
SLING_PG_DST_ENV="POSTGRES_BENCH_DST"
SLING_MYSQL_SRC_ENV="MYSQL_BENCH_SRC"
SLING_MONGO_SRC_ENV="MONGODB_BENCH_SRC"
SLING_DUCKDB_SRC_ENV="DUCKDB_BENCH_SRC"
SLING_DUCKDB_DST_ENV="DUCKDB_BENCH_DST"
SLING_MSSQL_DST_ENV="SQLSERVER_BENCH_DST"
