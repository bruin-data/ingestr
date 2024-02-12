import hashlib

import dlt
import typer

from ingestr.src.factory import SourceDestinationFactory
from rich import print
from dlt.common.pipeline import LoadInfo
import humanize

app = typer.Typer(name="ingestr")


@app.command()
def ingest(
    source_uri: str = None,  # type: ignore
    dest_uri: str = None,  # type: ignore
    source_table: str = None,  # type: ignore
    dest_table: str = None,  # type: ignore
    incremental_key: str = None,  # type: ignore
    incremental_strategy: str = "replace",  # type: ignore
):
    if not source_uri:
        typer.echo("Please provide a source URI")
        raise typer.Abort()

    if not dest_uri:
        typer.echo("Please provide a destination URI")
        raise typer.Abort()

    if not source_table:
        print("[bold red]Please provide a source table [\red bold]")
        raise typer.Abort()

    if not dest_table:
        typer.echo("Please provide a destination table")
        raise typer.Abort()


    factory = SourceDestinationFactory(source_uri, dest_uri)
    source = factory.get_source()
    destination = factory.get_destination()

    m = hashlib.sha256()
    m.update(dest_table.encode("utf-8"))

    pipeline = dlt.pipeline(
        pipeline_name=m.hexdigest(),
        destination=destination.dlt_dest(
            uri=dest_uri,
        ),
        progress=dlt.progress.log(dump_system_stats=False),
        pipelines_dir="pipeline_data",
    )

    print()
    print(f"[bold green]Initiated pipeline, starting...[/bold green]")
    print()

    incremental = []
    if incremental_key:
        incremental = [incremental_key]

    run_info = pipeline.run(
        source.dlt_source(
            uri=source_uri,
            table=source_table,
            incremental_key=incremental_key,
            incremental_strategy=incremental_strategy,
        ),
        **destination.dlt_run_params(
            uri=dest_uri,
            table=dest_table,
        ),
        write_disposition=incremental_strategy,  # type: ignore
        primary_key=incremental,
    )

    print()
    print(f"[bold green]Successfully finished loading data from '{factory.source_scheme}' to '{factory.destination_scheme}'. [/bold green]")
    # typer.echo(printLoadInfo(run_info))


def printLoadInfo(info: LoadInfo):
    msg = f"Pipeline {info.pipeline.pipeline_name} load step completed in "
    if info.started_at:
        elapsed = info.finished_at - info.started_at
        msg += humanize.precisedelta(elapsed)
    else:
        msg += "---"
    msg += (
        f"\n{len(info.loads_ids)} load package(s) were loaded to destination"
        f" {info.destination_name} and into dataset {info.dataset_name}\n"
    )
    if info.staging_name:
        msg += (
            f"The {info.staging_name} staging destination used"
            f" {info.staging_displayable_credentials} location to stage data\n"
        )

    msg += (
        f"The {info.destination_name} destination used"
        f" {info.destination_displayable_credentials} location to store data"
    )
    msg += info._load_packages_asstr(info.load_packages, 0)
    return msg


@app.command()
def example_uris():
    print()
    typer.echo(
        f"Following are some example URI formats for supported sources and destinations:"
    )

    print()
    print(
        f"[bold green]Postgres:[/bold green] [white]postgres://user:password@host:port/dbname?sslmode=require [/white]"
    )
    print(
        f"[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#postgresql[/white dim]"
    )

    print()
    print(
        f"[bold green]BigQuery:[/bold green] [white]bigquery://project-id?credentials_path=/path/to/credentials.json&location=US [/white]"
    )
    print(
        f"[white dim]└── https://github.com/googleapis/python-bigquery-sqlalchemy?tab=readme-ov-file#connection-string-parameters[/white dim]"
    )

    print()
    print(
        f"[bold green]Snowflake:[/bold green] [white]snowflake://user:password@account/dbname?warehouse=COMPUTE_WH [/white]"
    )
    print(
        f"[white dim]└── https://docs.snowflake.com/en/developer-guide/python-connector/sqlalchemy#connection-parameters"
    )

    print()
    print(
        f"[bold green]Redshift:[/bold green] [white]redshift://user:password@host:port/dbname?sslmode=require [/white]"
    )
    print(
        f"[white dim]└── https://aws.amazon.com/blogs/big-data/use-the-amazon-redshift-sqlalchemy-dialect-to-interact-with-amazon-redshift/[/white dim]"
    )

    print()
    print(
        f"[bold green]Databricks:[/bold green] [white]databricks://token:<access_token>@<server_hostname>?http_path=<http_path>&catalog=<catalog>&schema=<schema>[/white]"
    )
    print(f"[white dim]└── https://docs.databricks.com/en/dev-tools/sqlalchemy.html")

    print()
    print(
        f"[bold green]Microsoft SQL Server:[/bold green] [white]mssql://user:password@host:port/dbname?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=yes [/white]"
    )
    print(
        f"[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#microsoft-sql-server"
    )

    print()
    print(
        f"[bold green]MySQL:[/bold green] [white]mysql://user:password@host:port/dbname [/white]"
    )
    print(
        f"[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#mysql[/white dim]"
    )

    print()
    print(f"[bold green]DuckDB:[/bold green] [white]duckdb://path/to/database [/white]")
    print(f"[white dim]└── https://github.com/Mause/duckdb_engine[/white dim]")

    print()
    print(f"[bold green]SQLite:[/bold green] [white]sqlite://path/to/database [/white]")
    print(
        f"[white dim]└── https://docs.sqlalchemy.org/en/20/core/engines.html#sqlite[/white dim]"
    )

    print()
    typer.echo(
        "These are all coming from SQLAlchemy's URI format, so they should be familiar to most users."
    )


def main():
    app()


if __name__ == "__main__":
    main()
