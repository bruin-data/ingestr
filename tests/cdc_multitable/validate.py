#!/usr/bin/env python3
"""
Validation script for CDC multi-table testing.
Compares schema and data between source and destination databases.
"""

import psycopg2
import sys
import json
from typing import Dict, List, Any, Tuple, Optional


def get_connection(host: str, port: int, user: str, password: str, dbname: str):
    """Create a database connection."""
    return psycopg2.connect(
        host=host,
        port=port,
        user=user,
        password=password,
        dbname=dbname
    )


def get_table_schema(conn, table_name: str) -> List[Dict[str, Any]]:
    """Get column definitions for a table."""
    schema_name = "public"
    if "." in table_name:
        schema_name, table_name = table_name.split(".", 1)

    query = """
        SELECT
            column_name,
            data_type,
            is_nullable,
            column_default,
            character_maximum_length,
            numeric_precision,
            numeric_scale
        FROM information_schema.columns
        WHERE table_schema = %s AND table_name = %s
        ORDER BY ordinal_position
    """

    with conn.cursor() as cur:
        cur.execute(query, (schema_name, table_name))
        columns = []
        for row in cur.fetchall():
            columns.append({
                "name": row[0],
                "data_type": row[1],
                "is_nullable": row[2],
                "column_default": row[3],
                "max_length": row[4],
                "precision": row[5],
                "scale": row[6]
            })
        return columns


def get_table_data(conn, table_name: str, order_by: str = "id", exclude_cdc_columns: bool = False, only_active: bool = False) -> List[Tuple]:
    """Get all rows from a table, ordered by specified column."""
    with conn.cursor() as cur:
        # Get column names first
        cur.execute(f"SELECT * FROM {table_name} LIMIT 0")
        columns = [desc[0] for desc in cur.description]

        has_cdc_deleted = "_cdc_deleted" in columns

        if exclude_cdc_columns:
            # Filter out CDC metadata columns for comparison
            columns = [c for c in columns if not c.startswith("_cdc_")]

        columns_str = ", ".join(f'"{c}"' for c in columns)

        # Build WHERE clause for active rows only (in CDC destination tables)
        where_clause = ""
        if only_active and has_cdc_deleted:
            where_clause = 'WHERE "_cdc_deleted" = false'

        cur.execute(f"SELECT {columns_str} FROM {table_name} {where_clause} ORDER BY {order_by}")
        return cur.fetchall(), columns


def get_row_count(conn, table_name: str, only_active: bool = False) -> int:
    """Get row count for a table."""
    with conn.cursor() as cur:
        # Check if _cdc_deleted column exists
        cur.execute(f"SELECT * FROM {table_name} LIMIT 0")
        columns = [desc[0] for desc in cur.description]
        has_cdc_deleted = "_cdc_deleted" in columns

        where_clause = ""
        if only_active and has_cdc_deleted:
            where_clause = 'WHERE "_cdc_deleted" = false'

        cur.execute(f"SELECT COUNT(*) FROM {table_name} {where_clause}")
        return cur.fetchone()[0]


def compare_schemas(source_schema: List[Dict], dest_schema: List[Dict], table_name: str) -> Tuple[bool, List[str]]:
    """Compare schemas between source and destination."""
    errors = []

    # Get source column names (destination will have extra CDC columns)
    source_cols = {col["name"]: col for col in source_schema}
    dest_cols = {col["name"]: col for col in dest_schema}

    # Check that all source columns exist in destination
    for col_name, col_def in source_cols.items():
        if col_name not in dest_cols:
            errors.append(f"Column '{col_name}' missing in destination")
            continue

        dest_col = dest_cols[col_name]

        # Compare data types (allowing for some variations)
        if not types_compatible(col_def["data_type"], dest_col["data_type"]):
            errors.append(f"Column '{col_name}' type mismatch: source={col_def['data_type']}, dest={dest_col['data_type']}")

    # Check for expected CDC columns in destination
    cdc_columns = ["_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"]
    for cdc_col in cdc_columns:
        if cdc_col not in dest_cols:
            errors.append(f"Missing CDC column '{cdc_col}' in destination")

    return len(errors) == 0, errors


def types_compatible(source_type: str, dest_type: str) -> bool:
    """Check if two types are compatible."""
    # Normalize types
    source_type = source_type.lower()
    dest_type = dest_type.lower()

    if source_type == dest_type:
        return True

    # Common compatible types
    compatible_groups = [
        {"integer", "int", "int4"},
        {"bigint", "int8"},
        {"smallint", "int2"},
        {"text", "character varying", "varchar"},
        {"double precision", "float8", "real", "float4"},
        {"timestamp without time zone", "timestamp"},
        {"timestamp with time zone", "timestamptz"},
        {"boolean", "bool"},
    ]

    for group in compatible_groups:
        if source_type in group and dest_type in group:
            return True

    return False


def compare_data(source_data: List[Tuple], dest_data: List[Tuple],
                 source_cols: List[str], dest_cols: List[str],
                 table_name: str) -> Tuple[bool, List[str]]:
    """Compare data between source and destination."""
    errors = []

    if len(source_data) != len(dest_data):
        errors.append(f"Row count mismatch: source={len(source_data)}, dest={len(dest_data)}")
        return False, errors

    # Create column index mapping (source columns in dest)
    source_col_indices = list(range(len(source_cols)))
    dest_col_indices = []
    for src_col in source_cols:
        if src_col in dest_cols:
            dest_col_indices.append(dest_cols.index(src_col))
        else:
            errors.append(f"Source column '{src_col}' not found in destination")
            return False, errors

    for row_idx, (src_row, dest_row) in enumerate(zip(source_data, dest_data)):
        for src_col_idx, dest_col_idx in zip(source_col_indices, dest_col_indices):
            src_val = src_row[src_col_idx]
            dest_val = dest_row[dest_col_idx]

            if not values_equal(src_val, dest_val):
                col_name = source_cols[src_col_idx]
                errors.append(f"Row {row_idx}, column '{col_name}': source={repr(src_val)}, dest={repr(dest_val)}")
                if len(errors) > 10:
                    errors.append("... (truncated, too many errors)")
                    return False, errors

    return len(errors) == 0, errors


def values_equal(val1, val2) -> bool:
    """Compare two values for equality, handling type differences."""
    if val1 is None and val2 is None:
        return True
    if val1 is None or val2 is None:
        return False

    # Handle numeric comparisons
    if isinstance(val1, (int, float)) and isinstance(val2, (int, float)):
        return abs(val1 - val2) < 0.0001

    # Handle string comparisons
    if isinstance(val1, str) and isinstance(val2, str):
        return val1 == val2

    # Default comparison
    return val1 == val2


def check_cdc_columns(conn, table_name: str) -> Tuple[bool, Dict[str, Any]]:
    """Check CDC column values."""
    with conn.cursor() as cur:
        cur.execute(f"""
            SELECT
                COUNT(*) as total,
                COUNT(DISTINCT "_cdc_lsn") as distinct_lsns,
                SUM(CASE WHEN "_cdc_deleted" = true THEN 1 ELSE 0 END) as deleted_count,
                MIN("_cdc_synced_at") as min_synced,
                MAX("_cdc_synced_at") as max_synced
            FROM {table_name}
        """)
        row = cur.fetchone()
        return True, {
            "total_rows": row[0],
            "distinct_lsns": row[1],
            "deleted_count": row[2],
            "min_synced_at": str(row[3]) if row[3] else None,
            "max_synced_at": str(row[4]) if row[4] else None
        }


def validate_tables(source_conn, dest_conn, tables: List[str],
                    check_schema: bool = True, check_data: bool = True) -> Tuple[bool, Dict[str, Any]]:
    """Validate multiple tables between source and destination."""
    results = {}
    all_passed = True

    for table in tables:
        table_result = {
            "passed": True,
            "errors": [],
            "source_count": 0,
            "dest_count": 0,
            "cdc_info": None
        }

        try:
            # Get row counts (for destination, only count active rows)
            table_result["source_count"] = get_row_count(source_conn, table)
            table_result["dest_count"] = get_row_count(dest_conn, table, only_active=True)

            if check_schema:
                source_schema = get_table_schema(source_conn, table)
                dest_schema = get_table_schema(dest_conn, table)

                schema_ok, schema_errors = compare_schemas(source_schema, dest_schema, table)
                if not schema_ok:
                    table_result["passed"] = False
                    table_result["errors"].extend([f"Schema: {e}" for e in schema_errors])

            if check_data:
                source_data, source_cols = get_table_data(source_conn, table)
                # For CDC destinations, only compare active (non-deleted) rows
                dest_data, dest_cols = get_table_data(dest_conn, table, exclude_cdc_columns=True, only_active=True)

                data_ok, data_errors = compare_data(source_data, dest_data, source_cols, dest_cols, table)
                if not data_ok:
                    table_result["passed"] = False
                    table_result["errors"].extend([f"Data: {e}" for e in data_errors])

            # Check CDC columns
            cdc_ok, cdc_info = check_cdc_columns(dest_conn, table)
            table_result["cdc_info"] = cdc_info

        except Exception as e:
            table_result["passed"] = False
            table_result["errors"].append(f"Exception: {str(e)}")

        if not table_result["passed"]:
            all_passed = False

        results[table] = table_result

    return all_passed, results


def print_results(results: Dict[str, Any], phase: str):
    """Print validation results."""
    print(f"\n{'='*60}")
    print(f"Validation Results: {phase}")
    print(f"{'='*60}")

    all_passed = True
    for table, result in results.items():
        status = "PASS" if result["passed"] else "FAIL"
        all_passed = all_passed and result["passed"]

        print(f"\n[{status}] {table}")
        print(f"  Source rows: {result['source_count']}")
        print(f"  Dest rows:   {result['dest_count']}")

        if result["cdc_info"]:
            cdc = result["cdc_info"]
            print(f"  CDC Info: {cdc['distinct_lsns']} distinct LSNs, {cdc['deleted_count']} deleted")

        if result["errors"]:
            print(f"  Errors:")
            for error in result["errors"][:5]:
                print(f"    - {error}")
            if len(result["errors"]) > 5:
                print(f"    ... and {len(result['errors']) - 5} more")

    print(f"\n{'='*60}")
    print(f"Overall: {'ALL PASSED' if all_passed else 'SOME FAILED'}")
    print(f"{'='*60}\n")

    return all_passed


def main():
    if len(sys.argv) < 2:
        print("Usage: validate.py <phase_name> [tables...]")
        print("Example: validate.py 'After Snapshot' users orders products")
        sys.exit(1)

    phase = sys.argv[1]
    tables = sys.argv[2:] if len(sys.argv) > 2 else ["users", "orders", "products"]

    # Connection parameters
    source_params = {
        "host": "localhost",
        "port": 5433,
        "user": "source_user",
        "password": "source_pass",
        "dbname": "source_db"
    }

    dest_params = {
        "host": "localhost",
        "port": 5434,
        "user": "dest_user",
        "password": "dest_pass",
        "dbname": "dest_db"
    }

    try:
        source_conn = get_connection(**source_params)
        dest_conn = get_connection(**dest_params)

        all_passed, results = validate_tables(source_conn, dest_conn, tables)

        passed = print_results(results, phase)

        source_conn.close()
        dest_conn.close()

        sys.exit(0 if passed else 1)

    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
