from datetime import datetime
from enum import Enum
from typing import Optional

import typer
from rich.console import Console
from typing_extensions import Annotated

from ingestr.src.telemetry.event import track

app = typer.Typer(
    name="ingestr",
    help="ingestr is the CLI tool to ingest data from one source to another",
    rich_markup_mode="rich",
    pretty_exceptions_enable=False,
)

console = Console()
print = console.print

DATE_FORMATS = [
    "%Y-%m-%d",
    "%Y-%m-%dT%H:%M:%S",
    "%Y-%m-%dT%H:%M:%S%z",
    "%Y-%m-%d %H:%M:%S",
    "%Y-%m-%dT%H:%M:%S.%f",
    "%Y-%m-%dT%H:%M:%S.%f%z",
]

# https://dlthub.com/docs/dlt-ecosystem/file-formats/parquet#supported-destinations
PARQUET_SUPPORTED_DESTINATIONS = [
    "athenabigquery",
    "duckdb",
    "snowflake",
    "databricks",
    "synapse",
    "s3",
]

# these sources would return a JSON for sure, which means they cannot be used with Parquet loader for BigQuery
JSON_RETURNING_SOURCES = ["notion"]


class IncrementalStrategy(str, Enum):
    create_replace = "replace"
    append = "append"
    delete_insert = "delete+insert"
    merge = "merge"
    scd2 = "scd2"
    none = "none"


class LoaderFileFormat(str, Enum):
    jsonl = "jsonl"
    parquet = "parquet"
    insert_values = "insert_values"
    csv = "csv"


class SqlBackend(str, Enum):
    sqlalchemy = "sqlalchemy"
    pyarrow = "pyarrow"
    connectorx = "connectorx"


class Progress(str, Enum):
    interactive = "interactive"
    log = "log"


class SchemaNaming(str, Enum):
    default = "default"
    direct = "direct"


class SqlReflectionLevel(str, Enum):
    minimal = "minimal"
    full = "full"
    full_with_precision = "full_with_precision"


@app.command()
def ingest(
    source_uri: Annotated[
        str,
        typer.Option(
            help="The URI of the [green]source[/green]",
            envvar=["SOURCE_URI", "INGESTR_SOURCE_URI"],
        ),
    ],  # type: ignore
    dest_uri: Annotated[
        str,
        typer.Option(
            help="The URI of the [cyan]destination[/cyan]",
            envvar=["DESTINATION_URI", "INGESTR_DESTINATION_URI"],
        ),
    ],  # type: ignore
    source_table: Annotated[
        str,
        typer.Option(
            help="The table name in the [green]source[/green] to fetch",
            envvar=["SOURCE_TABLE", "INGESTR_SOURCE_TABLE"],
        ),
    ],  # type: ignore
    dest_table: Annotated[
        str,
        typer.Option(
            help="The table in the [cyan]destination[/cyan] to save the data into",
            envvar=["DESTINATION_TABLE", "INGESTR_DESTINATION_TABLE"],
        ),
    ] = None,  # type: ignore
    incremental_key: Annotated[
        Optional[str],
        typer.Option(
            help="The incremental key from the table to be used for incremental strategies",
            envvar=["INCREMENTAL_KEY", "INGESTR_INCREMENTAL_KEY"],
        ),
    ] = None,  # type: ignore
    incremental_strategy: Annotated[
        IncrementalStrategy,
        typer.Option(
            help="The incremental strategy to use",
            envvar=["INCREMENTAL_STRATEGY", "INGESTR_INCREMENTAL_STRATEGY"],
        ),
    ] = IncrementalStrategy.create_replace,  # type: ignore
    interval_start: Annotated[
        Optional[datetime],
        typer.Option(
            help="The start of the interval the incremental key will cover",
            formats=DATE_FORMATS,
            envvar=["INTERVAL_START", "INGESTR_INTERVAL_START"],
        ),
    ] = None,  # type: ignore
    interval_end: Annotated[
        Optional[datetime],
        typer.Option(
            help="The end of the interval the incremental key will cover",
            formats=DATE_FORMATS,
            envvar=["INTERVAL_END", "INGESTR_INTERVAL_END"],
        ),
    ] = None,  # type: ignore
    primary_key: Annotated[
        Optional[list[str]],
        typer.Option(
            help="The key that will be used to deduplicate the resulting table",
            envvar=["PRIMARY_KEY", "INGESTR_PRIMARY_KEY"],
        ),
    ] = None,  # type: ignore
    partition_by: Annotated[
        Optional[str],
        typer.Option(
            help="The partition key to be used for partitioning the destination table",
            envvar=["PARTITION_BY", "INGESTR_PARTITION_BY"],
        ),
    ] = None,  # type: ignore
    cluster_by: Annotated[
        Optional[str],
        typer.Option(
            help="The clustering key to be used for clustering the destination table, not every destination supports clustering.",
            envvar=["CLUSTER_BY", "INGESTR_CLUSTER_BY"],
        ),
    ] = None,  # type: ignore
    yes: Annotated[
        Optional[bool],
        typer.Option(
            help="Skip the confirmation prompt and ingest right away",
            envvar=["SKIP_CONFIRMATION", "INGESTR_SKIP_CONFIRMATION"],
        ),
    ] = False,  # type: ignore
    full_refresh: Annotated[
        bool,
        typer.Option(
            help="Ignore the state and refresh the destination table completely",
            envvar=["FULL_REFRESH", "INGESTR_FULL_REFRESH"],
        ),
    ] = False,  # type: ignore
    progress: Annotated[
        Progress,
        typer.Option(
            help="The progress display type, must be one of 'interactive', 'log'",
            envvar=["PROGRESS", "INGESTR_PROGRESS"],
        ),
    ] = Progress.interactive,  # type: ignore
    sql_backend: Annotated[
        SqlBackend,
        typer.Option(
            help="The SQL backend to use",
            envvar=["SQL_BACKEND", "INGESTR_SQL_BACKEND"],
        ),
    ] = SqlBackend.pyarrow,  # type: ignore
    loader_file_format: Annotated[
        Optional[LoaderFileFormat],
        typer.Option(
            help="The file format to use when loading data",
            envvar=["LOADER_FILE_FORMAT", "INGESTR_LOADER_FILE_FORMAT"],
        ),
    ] = None,  # type: ignore
    page_size: Annotated[
        Optional[int],
        typer.Option(
            help="The page size to be used when fetching data from SQL sources",
            envvar=["PAGE_SIZE", "INGESTR_PAGE_SIZE"],
        ),
    ] = 50000,  # type: ignore
    loader_file_size: Annotated[
        Optional[int],
        typer.Option(
            help="The file size to be used by the loader to split the data into multiple files. This can be set independent of the page size, since page size is used for fetching the data from the sources whereas this is used for the processing/loading part.",
            envvar=["LOADER_FILE_SIZE", "INGESTR_LOADER_FILE_SIZE"],
        ),
    ] = 100000,  # type: ignore
    schema_naming: Annotated[
        SchemaNaming,
        typer.Option(
            help="The naming convention to use when moving the tables from source to destination. The default behavior is explained here: https://dlthub.com/docs/general-usage/schema#naming-convention",
            envvar=["SCHEMA_NAMING", "INGESTR_SCHEMA_NAMING"],
        ),
    ] = SchemaNaming.default,  # type: ignore
    pipelines_dir: Annotated[
        Optional[str],
        typer.Option(
            help="The path to store dlt-related pipeline metadata. By default, ingestr will create a temporary directory and delete it after the execution is done in order to make retries stateless.",
            envvar=["PIPELINES_DIR", "INGESTR_PIPELINES_DIR"],
        ),
    ] = None,  # type: ignore
    extract_parallelism: Annotated[
        Optional[int],
        typer.Option(
            help="The number of parallel jobs to run for extracting data from the source, only applicable for certain sources",
            envvar=["EXTRACT_PARALLELISM", "INGESTR_EXTRACT_PARALLELISM"],
        ),
    ] = 5,  # type: ignore
    sql_reflection_level: Annotated[
        SqlReflectionLevel,
        typer.Option(
            help="The reflection level to use when reflecting the table schema from the source",
            envvar=["SQL_REFLECTION_LEVEL", "INGESTR_SQL_REFLECTION_LEVEL"],
        ),
    ] = SqlReflectionLevel.full,  # type: ignore
    sql_limit: Annotated[
        Optional[int],
        typer.Option(
            help="The limit to use when fetching data from the source",
            envvar=["SQL_LIMIT", "INGESTR_SQL_LIMIT"],
        ),
    ] = None,  # type: ignore
    sql_exclude_columns: Annotated[
        Optional[list[str]],
        typer.Option(
            help="The columns to exclude from the source table",
            envvar=["SQL_EXCLUDE_COLUMNS", "INGESTR_SQL_EXCLUDE_COLUMNS"],
        ),
    ] = [],  # type: ignore
    columns: Annotated[
        Optional[list[str]],
        typer.Option(
            help="The column types to be used for the destination table in the format of 'column_name:column_type'",
            envvar=["COLUMNS", "INGESTR_COLUMNS"],
        ),
    ] = None,  # type: ignore
    yield_limit: Annotated[
        Optional[int],
        typer.Option(
            help="Limit the number of pages yielded from the source",
            envvar=["YIELD_LIMIT", "INGESTR_YIELD_LIMIT"],
        ),
    ] = None,  # type: ignore
    staging_bucket: Annotated[
        Optional[str],
        typer.Option(
            help="The staging bucket to be used for the ingestion, must be prefixed with 'gs://' or 's3://'",
            envvar=["STAGING_BUCKET", "INGESTR_STAGING_BUCKET"],
        ),
    ] = None,  # type: ignore
):
    import hashlib
    import tempfile
    from datetime import datetime

    import dlt
    import humanize
    import typer
    from dlt.common.pipeline import LoadInfo
    from dlt.common.runtime.collector import Collector, LogCollector
    from dlt.common.schema.typing import TColumnSchema

    import ingestr.src.partition as partition
    import ingestr.src.resource as resource
    from ingestr.src.collector.spinner import SpinnerCollector
    from ingestr.src.destinations import AthenaDestination
    from ingestr.src.factory import SourceDestinationFactory
    from ingestr.src.filters import cast_set_to_list, handle_mysql_empty_dates
    from ingestr.src.sources import MongoDbSource

    def report_errors(run_info: LoadInfo):
        for load_package in run_info.load_packages:
            failed_jobs = load_package.jobs["failed_jobs"]
            if len(failed_jobs) == 0:
                continue

            print()
            print("[bold red]Failed jobs:[/bold red]")
            print()
            for job in failed_jobs:
                print(f"[bold red]  {job.job_file_info.job_id()}[/bold red]")
                print(f"    [bold yellow]Error:[/bold yellow] {job.failed_message}")

            raise typer.Exit(1)

    def validate_source_dest_tables(
        source_table: str, dest_table: str
    ) -> tuple[str, str]:
        if not dest_table:
            if len(source_table.split(".")) != 2:
                print(
                    "[red]Table name must be in the format schema.table for source table when dest-table is not given.[/red]"
                )
                raise typer.Abort()

            print()
            print(
                "[yellow]Destination table is not given, defaulting to the source table.[/yellow]"
            )
            dest_table = source_table
        return (source_table, dest_table)

    def validate_loader_file_format(
        dlt_dest, loader_file_format: Optional[LoaderFileFormat]
    ):
        if (
            loader_file_format
            and loader_file_format.value
            not in dlt_dest.capabilities().supported_loader_file_formats
        ):
            print(
                f"[red]Loader file format {loader_file_format.value} is not supported by the destination, available formats: {dlt_dest.capabilities().supported_loader_file_formats}.[/red]"
            )
            raise typer.Abort()

    def parse_columns(columns: list[str]) -> dict:
        from typing import cast, get_args

        from dlt.common.data_types import TDataType

        possible_types = get_args(TDataType)

        types: dict[str, TDataType] = {}
        for column in columns:
            for candidate in column.split(","):
                column_name, column_type = candidate.split(":")
                if column_type not in possible_types:
                    print(
                        f"[red]Column type '{column_type}' is not supported, supported types: {possible_types}.[/red]"
                    )
                    raise typer.Abort()
                types[column_name] = cast(TDataType, column_type)
        return types

    track(
        "command_triggered",
        {
            "command": "ingest",
        },
    )

    clean_sql_exclude_columns = []
    if sql_exclude_columns:
        for col in sql_exclude_columns:
            for possible_col in col.split(","):
                clean_sql_exclude_columns.append(possible_col.strip())
        sql_exclude_columns = clean_sql_exclude_columns

    dlt.config["data_writer.buffer_max_items"] = page_size
    dlt.config["data_writer.file_max_items"] = loader_file_size
    dlt.config["extract.workers"] = extract_parallelism
    dlt.config["extract.max_parallel_items"] = extract_parallelism
    dlt.config["load.raise_on_max_retries"] = 15
    if schema_naming != SchemaNaming.default:
        dlt.config["schema.naming"] = schema_naming.value

    try:
        (source_table, dest_table) = validate_source_dest_tables(
            source_table, dest_table
        )

        factory = SourceDestinationFactory(source_uri, dest_uri)
        track(
            "command_running",
            {
                "command": "ingest",
                "source_type": factory.source_scheme,
                "destination_type": factory.destination_scheme,
            },
        )

        source = factory.get_source()
        destination = factory.get_destination()

        column_hints: dict[str, TColumnSchema] = {}
        original_incremental_strategy = incremental_strategy

        if columns:
            column_types = parse_columns(columns)
            for column_name, column_type in column_types.items():
                column_hints[column_name] = {"data_type": column_type}

        merge_key = None
        if incremental_strategy == IncrementalStrategy.delete_insert:
            merge_key = incremental_key
            incremental_strategy = IncrementalStrategy.merge
            if incremental_key:
                if incremental_key not in column_hints:
                    column_hints[incremental_key] = {}

                column_hints[incremental_key]["merge_key"] = True

        m = hashlib.sha256()
        m.update(dest_table.encode("utf-8"))

        progressInstance: Collector = SpinnerCollector()
        if progress == Progress.log:
            progressInstance = LogCollector()

        is_pipelines_dir_temp = False
        if pipelines_dir is None:
            pipelines_dir = tempfile.mkdtemp()
            is_pipelines_dir_temp = True

        dlt_dest = destination.dlt_dest(
            uri=dest_uri, dest_table=dest_table, staging_bucket=staging_bucket
        )
        validate_loader_file_format(dlt_dest, loader_file_format)

        if partition_by:
            if partition_by not in column_hints:
                column_hints[partition_by] = {}

            column_hints[partition_by]["partition"] = True

        if cluster_by:
            if cluster_by not in column_hints:
                column_hints[cluster_by] = {}

            column_hints[cluster_by]["cluster"] = True

        if primary_key:
            for key in primary_key:
                if key not in column_hints:
                    column_hints[key] = {}

                column_hints[key]["primary_key"] = True

        pipeline = dlt.pipeline(
            pipeline_name=m.hexdigest(),
            destination=dlt_dest,
            progress=progressInstance,
            pipelines_dir=pipelines_dir,
            refresh="drop_resources" if full_refresh else None,
        )

        if source.handles_incrementality():
            incremental_strategy = IncrementalStrategy.none
            incremental_key = None

        incremental_strategy_text = (
            incremental_strategy.value
            if incremental_strategy.value != IncrementalStrategy.none
            else "Platform-specific"
        )

        source_table_print = source_table.split(":")[0]

        print()
        print("[bold green]Initiated the pipeline with the following:[/bold green]")
        print(
            f"[bold yellow]  Source:[/bold yellow] {factory.source_scheme} / {source_table_print}"
        )
        print(
            f"[bold yellow]  Destination:[/bold yellow] {factory.destination_scheme} / {dest_table}"
        )
        print(
            f"[bold yellow]  Incremental Strategy:[/bold yellow] {incremental_strategy_text}"
        )
        print(
            f"[bold yellow]  Incremental Key:[/bold yellow] {incremental_key if incremental_key else 'None'}"
        )
        print(
            f"[bold yellow]  Primary Key:[/bold yellow] {primary_key if primary_key else 'None'}"
        )
        print(f"[bold yellow]  Pipeline ID:[/bold yellow] {m.hexdigest()}")
        print()

        if not yes:
            continuePipeline = typer.confirm("Are you sure you would like to continue?")
            if not continuePipeline:
                track("command_finished", {"command": "ingest", "status": "aborted"})
                raise typer.Abort()

        print()
        print("[bold green]Starting the ingestion...[/bold green]")

        if factory.source_scheme == "sqlite":
            source_table = "main." + source_table.split(".")[-1]

        if (
            incremental_key
            and incremental_key in column_hints
            and "data_type" in column_hints[incremental_key]
            and column_hints[incremental_key]["data_type"] == "date"
        ):
            # By default, ingestr treats the start and end dates as datetime objects. While this worked fine for many cases, if the
            # incremental field is a date, the start and end dates cannot be compared to the incremental field, and the ingestion would fail.
            # In order to eliminate this, we have introduced a new option to ingestr, --columns, which allows the user to specify the column types for the destination table.
            # This way, ingestr will know the data type of the incremental field, and will be able to convert the start and end dates to the correct data type before running the ingestion.
            if interval_start:
                interval_start = interval_start.date()  # type: ignore
            if interval_end:
                interval_end = interval_end.date()  # type: ignore

        dlt_source = source.dlt_source(
            uri=source_uri,
            table=source_table,
            incremental_key=incremental_key,
            merge_key=merge_key,
            interval_start=interval_start,
            interval_end=interval_end,
            sql_backend=sql_backend.value,
            page_size=page_size,
            sql_reflection_level=sql_reflection_level.value,
            sql_limit=sql_limit,
            sql_exclude_columns=sql_exclude_columns,
        )

        resource.for_each(dlt_source, lambda x: x.add_map(cast_set_to_list))
        if factory.source_scheme.startswith("mysql"):
            resource.for_each(dlt_source, lambda x: x.add_map(handle_mysql_empty_dates))

        if yield_limit:
            resource.for_each(dlt_source, lambda x: x.add_limit(yield_limit))

        if isinstance(source, MongoDbSource):
            from ingestr.src.resource import TypeHintMap

            resource.for_each(
                dlt_source, lambda x: x.add_map(TypeHintMap().type_hint_map)
            )

        def col_h(x):
            if column_hints:
                x.apply_hints(columns=column_hints)

        resource.for_each(dlt_source, col_h)

        if isinstance(destination, AthenaDestination) and partition_by:
            partition.apply_athena_hints(dlt_source, partition_by, column_hints)

        if original_incremental_strategy == IncrementalStrategy.delete_insert:

            def set_primary_key(x):
                x.incremental.primary_key = ()

            resource.for_each(dlt_source, set_primary_key)

        if (
            factory.destination_scheme in PARQUET_SUPPORTED_DESTINATIONS
            and loader_file_format is None
        ):
            loader_file_format = LoaderFileFormat.parquet

            # if the source is a JSON returning source, we cannot use Parquet loader for BigQuery
            if (
                factory.destination_scheme == "bigquery"
                and factory.source_scheme in JSON_RETURNING_SOURCES
            ):
                loader_file_format = None

        write_disposition = None
        if incremental_strategy != IncrementalStrategy.none:
            write_disposition = incremental_strategy.value

        start_time = datetime.now()

        run_info: LoadInfo = pipeline.run(
            dlt_source,
            **destination.dlt_run_params(
                uri=dest_uri,
                table=dest_table,
                staging_bucket=staging_bucket,
            ),
            write_disposition=write_disposition,  # type: ignore
            primary_key=(primary_key if primary_key and len(primary_key) > 0 else None),  # type: ignore
            loader_file_format=(
                loader_file_format.value if loader_file_format is not None else None  # type: ignore
            ),  # type: ignore
        )

        report_errors(run_info)

        destination.post_load()

        end_time = datetime.now()
        elapsedHuman = ""
        elapsed = end_time - start_time
        elapsedHuman = f"in {humanize.precisedelta(elapsed)}"

        if is_pipelines_dir_temp:
            import shutil

            shutil.rmtree(pipelines_dir)

        print(
            f"[bold green]Successfully finished loading data from '{factory.source_scheme}' to '{factory.destination_scheme}' {elapsedHuman} [/bold green]"
        )
        print()
        track(
            "command_finished",
            {
                "command": "ingest",
                "status": "success",
            },
        )

    except Exception as e:
        track(
            "command_finished",
            {"command": "ingest", "status": "failed", "error": str(e)},
        )
        raise


@app.command()
def example_uris():
    track(
        "command_triggered",
        {
            "command": "example-uris",
        },
    )

    print()
    typer.echo(
        "Following are some example URI formats for supported sources and destinations:"
    )

    print()
    print(
        "[bold green]Postgres:[/bold green] [white]postgres://user:password@host:port/dbname?sslmode=require [/white]"
    )
    print(
        "[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#postgresql[/white dim]"
    )

    print()
    print(
        "[bold green]BigQuery:[/bold green] [white]bigquery://project-id?credentials_path=/path/to/credentials.json&location=US [/white]"
    )
    print(
        "[white dim]└── https://github.com/googleapis/python-bigquery-sqlalchemy?tab=readme-ov-file#connection-string-parameters[/white dim]"
    )

    print()
    print(
        "[bold green]Snowflake:[/bold green] [white]snowflake://user:password@account/dbname?warehouse=COMPUTE_WH [/white]"
    )
    print(
        "[white dim]└── https://docs.snowflake.com/en/developer-guide/python-connector/sqlalchemy#connection-parameters"
    )

    print()
    print(
        "[bold green]Redshift:[/bold green] [white]redshift://user:password@host:port/dbname?sslmode=require [/white]"
    )
    print(
        "[white dim]└── https://aws.amazon.com/blogs/big-data/use-the-amazon-redshift-sqlalchemy-dialect-to-interact-with-amazon-redshift/[/white dim]"
    )

    print()
    print(
        "[bold green]Databricks:[/bold green] [white]databricks://token:<access_token>@<server_hostname>?http_path=<http_path>&catalog=<catalog>&schema=<schema>[/white]"
    )
    print("[white dim]└── https://docs.databricks.com/en/dev-tools/sqlalchemy.html")

    print()
    print(
        "[bold green]Microsoft SQL Server:[/bold green] [white]mssql://user:password@host:port/dbname?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=yes [/white]"
    )
    print(
        "[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#microsoft-sql-server"
    )

    print()
    print(
        "[bold green]MySQL:[/bold green] [white]mysql://user:password@host:port/dbname [/white]"
    )
    print(
        "[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#mysql[/white dim]"
    )

    print()
    print("[bold green]DuckDB:[/bold green] [white]duckdb://path/to/database [/white]")
    print("[white dim]└── https://github.com/Mause/duckdb_engine[/white dim]")

    print()
    print("[bold green]SQLite:[/bold green] [white]sqlite://path/to/database [/white]")
    print(
        "[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#sqlite[/white dim]"
    )

    print()
    typer.echo(
        "These are all coming from SQLAlchemy's URI format, so they should be familiar to most users."
    )
    track(
        "command_finished",
        {
            "command": "example-uris",
            "status": "success",
        },
    )


@app.command()
def version():
    from ingestr.src.version import __version__  # type: ignore

    print(f"v{__version__}")


def main():
    app()


if __name__ == "__main__":
    main()
