# /// script
# requires-python = ">=3.11,<3.12"
# dependencies = [
#     "pyspark>=3.5,<3.6",
# ]
# ///
"""Single-node Spark ingestion benchmark.

JDBC range partitioning is opt-in. Set BENCH_SPARK_PARTITIONED_READ=true to
enable it, then optionally tune BENCH_SPARK_PARTITION_COLUMN,
BENCH_SPARK_LOWER_BOUND, and BENCH_SPARK_NUM_PARTITIONS.
"""

import argparse
import os
import sys
from urllib.parse import parse_qsl, urlencode, unquote, urlparse

os.environ.setdefault("PYSPARK_PYTHON", sys.executable)
os.environ.setdefault("PYSPARK_DRIVER_PYTHON", sys.executable)
os.environ.setdefault("SPARK_LOCAL_IP", "127.0.0.1")

from pyspark.sql import SparkSession


JDBC_PACKAGES = {
    "postgres": "org.postgresql:postgresql:42.7.3",
    "mysql": "com.mysql:mysql-connector-j:8.4.0",
    "mssql": "com.microsoft.sqlserver:mssql-jdbc:12.6.1.jre11",
    "sqlserver": "com.microsoft.sqlserver:mssql-jdbc:12.6.1.jre11",
    "duckdb": "org.duckdb:duckdb_jdbc:1.0.0",
}

DUCKDB_COLUMN_TYPES = (
    "id INTEGER, "
    "small_str STRING, "
    "medium_str STRING, "
    "large_str STRING, "
    "tiny_int SMALLINT, "
    "regular_int INTEGER, "
    "big_int BIGINT, "
    "float_val DOUBLE, "
    "decimal_val DECIMAL(18,4), "
    "bool_val BOOLEAN, "
    "date_val DATE, "
    "ts_val TIMESTAMP, "
    "ts_tz_val TIMESTAMP, "
    "json_val STRING, "
    "extra_text STRING"
)


def int_env(name: str, default: int) -> int:
    value = os.environ.get(name)
    if not value:
        return default
    return int(value)


def bool_env(name: str, default: bool = False) -> bool:
    value = os.environ.get(name)
    if value is None:
        return default
    if value.lower() in ("1", "true", "yes", "on"):
        return True
    if value.lower() in ("0", "false", "no", "off"):
        return False
    raise ValueError(f"{name} must be a boolean value")


def default_partitions() -> int:
    return max(os.cpu_count() or 1, 1)


def required_packages(source_type: str, dest_type: str) -> str:
    override = os.environ.get("BENCH_SPARK_JARS_PACKAGES")
    if override is not None:
        return override

    packages = []
    for db_type in (source_type, dest_type):
        package = JDBC_PACKAGES.get(db_type)
        if package and package not in packages:
            packages.append(package)
    return ",".join(packages)


def query_string(params: dict[str, str]) -> str:
    return urlencode({k: v for k, v in params.items() if v is not None})


def parse_credentials(parsed) -> dict[str, str]:
    credentials = {}
    if parsed.username:
        credentials["user"] = unquote(parsed.username)
    if parsed.password:
        credentials["password"] = unquote(parsed.password)
    return credentials


def postgres_config(uri: str) -> dict:
    parsed = urlparse(uri.replace("postgres://", "postgresql://", 1))
    database = parsed.path.lstrip("/")
    params = dict(parse_qsl(parsed.query, keep_blank_values=True))
    url = f"jdbc:postgresql://{parsed.hostname}:{parsed.port or 5432}/{database}"
    if params:
        url += f"?{query_string(params)}"
    return {
        "url": url,
        "driver": "org.postgresql.Driver",
        "properties": parse_credentials(parsed),
    }


def mysql_config(uri: str) -> dict:
    parsed = urlparse(uri)
    database = parsed.path.lstrip("/")
    params = dict(parse_qsl(parsed.query, keep_blank_values=True))
    params.setdefault("useSSL", "false")
    params.setdefault("allowPublicKeyRetrieval", "true")
    params.setdefault("useCursorFetch", "true")
    url = f"jdbc:mysql://{parsed.hostname}:{parsed.port or 3306}/{database}"
    if params:
        url += f"?{query_string(params)}"
    return {
        "url": url,
        "driver": "com.mysql.cj.jdbc.Driver",
        "properties": parse_credentials(parsed),
    }


def mssql_config(uri: str) -> dict:
    parsed = urlparse(uri.replace("sqlserver://", "mssql://", 1))
    database = parsed.path.lstrip("/") or "master"
    params = dict(parse_qsl(parsed.query, keep_blank_values=True))
    encrypt = params.pop("encrypt", params.pop("Encrypt", "false"))
    if encrypt.lower() in ("disable", "false", "no", "0"):
        encrypt = "false"

    params.setdefault("encrypt", encrypt)
    params.setdefault("trustServerCertificate", "true")
    params.setdefault("databaseName", database)

    props = [f"{key}={value}" for key, value in params.items()]
    url = f"jdbc:sqlserver://{parsed.hostname}:{parsed.port or 1433};" + ";".join(props)
    return {
        "url": url,
        "driver": "com.microsoft.sqlserver.jdbc.SQLServerDriver",
        "properties": parse_credentials(parsed),
    }


def duckdb_path_from_uri(uri: str) -> str:
    return uri.split("duckdb:///", 1)[1]


def duckdb_config(uri: str) -> dict:
    return {
        "url": f"jdbc:duckdb:{duckdb_path_from_uri(uri)}",
        "driver": "org.duckdb.DuckDBDriver",
        "properties": {},
    }


def jdbc_config(db_type: str, uri: str) -> dict:
    if db_type == "postgres":
        return postgres_config(uri)
    if db_type == "mysql":
        return mysql_config(uri)
    if db_type in ("mssql", "sqlserver"):
        return mssql_config(uri)
    if db_type == "duckdb":
        return duckdb_config(uri)
    raise ValueError(f"Unsupported Spark benchmark database type: {db_type}")


def jdbc_options(config: dict, table: str) -> dict[str, str]:
    options = {
        "url": config["url"],
        "dbtable": table,
        "driver": config["driver"],
    }
    options.update(config["properties"])
    return options


def duckdb_source_table(table: str) -> str:
    return (
        "(SELECT "
        "id, small_str, medium_str, large_str, tiny_int, "
        "regular_int, big_int, float_val, decimal_val, bool_val, "
        "date_val, ts_val, CAST(ts_tz_val AS TIMESTAMP) AS ts_tz_val, "
        "json_val, extra_text "
        f"FROM {table}) AS bench_source"
    )


def create_spark(source_type: str, dest_type: str) -> SparkSession:
    partitions = str(int_env("BENCH_SPARK_SQL_SHUFFLE_PARTITIONS", default_partitions()))
    builder = (
        SparkSession.builder.appName("bench_spark_ingestion")
        .master(os.environ.get("BENCH_SPARK_MASTER", "local[*]"))
        .config("spark.ui.enabled", os.environ.get("BENCH_SPARK_UI", "false"))
        .config("spark.driver.memory", os.environ.get("BENCH_SPARK_DRIVER_MEMORY", "4g"))
        .config("spark.sql.shuffle.partitions", partitions)
        .config("spark.sql.session.timeZone", "UTC")
        .config("spark.driver.extraJavaOptions", "-Duser.timezone=UTC")
        .config("spark.executor.extraJavaOptions", "-Duser.timezone=UTC")
    )

    packages = required_packages(source_type, dest_type)
    if packages:
        builder = builder.config("spark.jars.packages", packages)

    return builder.getOrCreate()


def read_source(spark: SparkSession, args) -> object:
    config = jdbc_config(args.source_type, args.source_uri)
    options = jdbc_options(config, args.source_table)
    options["fetchsize"] = os.environ.get("BENCH_SPARK_FETCH_SIZE", "10000")

    if args.source_type == "duckdb":
        options["dbtable"] = duckdb_source_table(args.source_table)

    if args.source_type in ("postgres", "mysql"):
        options["customSchema"] = "json_val STRING"

    rows = args.rows or int(os.environ.get("BENCH_ROWS") or "0")
    partitioned_read = bool_env("BENCH_SPARK_PARTITIONED_READ")
    partition_column = os.environ.get("BENCH_SPARK_PARTITION_COLUMN", "id")
    if partitioned_read and rows > 1 and partition_column:
        options["partitionColumn"] = partition_column
        options["lowerBound"] = os.environ.get("BENCH_SPARK_LOWER_BOUND", "1")
        options["upperBound"] = str(rows)
        options["numPartitions"] = os.environ.get(
            "BENCH_SPARK_NUM_PARTITIONS",
            str(default_partitions()),
        )

    return spark.read.format("jdbc").options(**options).load()


def write_destination(df, args):
    config = jdbc_config(args.dest_type, args.dest_uri)
    options = jdbc_options(config, args.dest_table)
    options["batchsize"] = os.environ.get("BENCH_SPARK_BATCH_SIZE", "10000")

    output = df
    if args.dest_type == "duckdb":
        options["createTableColumnTypes"] = DUCKDB_COLUMN_TYPES
        output = df.coalesce(1)

    output.write.format("jdbc").mode("overwrite").options(**options).save()


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-type", required=True)
    parser.add_argument("--source-uri")
    parser.add_argument("--source-uri-env")
    parser.add_argument("--source-table", required=True)
    parser.add_argument("--dest-type", required=True)
    parser.add_argument("--dest-uri")
    parser.add_argument("--dest-uri-env")
    parser.add_argument("--dest-table", required=True)
    parser.add_argument("--rows", type=int, default=0)
    args = parser.parse_args()

    if args.source_uri_env:
        args.source_uri = os.environ.get(args.source_uri_env)
    if args.dest_uri_env:
        args.dest_uri = os.environ.get(args.dest_uri_env)
    if not args.source_uri:
        raise ValueError("--source-uri or --source-uri-env is required")
    if not args.dest_uri:
        raise ValueError("--dest-uri or --dest-uri-env is required")

    return args


def main():
    args = parse_args()
    spark = create_spark(args.source_type, args.dest_type)
    spark.sparkContext.setLogLevel(os.environ.get("BENCH_SPARK_LOG_LEVEL", "ERROR"))
    try:
        df = read_source(spark, args)
        write_destination(df, args)
    finally:
        spark.stop()


if __name__ == "__main__":
    main()
