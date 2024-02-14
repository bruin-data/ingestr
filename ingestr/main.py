import hashlib

import dlt
import typer

from ingestr.src.factory import SourceDestinationFactory
from rich.console import Console
from dlt.common.pipeline import LoadInfo
import humanize
from typing_extensions import Annotated

app = typer.Typer(
    name="ingestr",
    help="ingestr is the CLI tool to ingest data from one source to another",
    rich_markup_mode="rich",
)


console = Console()
print = console.print


@app.command()
def ingest(
    source_uri: Annotated[str, typer.Option(help="The URI of the [green]source[/green]")],  # type: ignore
    dest_uri: Annotated[str, typer.Option(help="The URI of the [cyan]destination[/cyan]")],  # type: ignore
    source_table: Annotated[str, typer.Option(help="The table name in the [green]source[/green] to fetch")],  # type: ignore
    dest_table: Annotated[str, typer.Option(help="The table in the [cyan]destination[/cyan] to save the data into")] = None,  # type: ignore
    incremental_key: Annotated[str, typer.Option(help="The incremental key from the table to be used for incremental strategies")] = None,  # type: ignore
    incremental_strategy: Annotated[str, typer.Option(help="The incremental strategy to use, must be one of 'replace', 'append' or 'merge'")] = "replace",  # type: ignore
):
    if not dest_table:
        print()
        print(
            "[yellow]Destination table is not given, defaulting to the source table.[/yellow]"
        )
        dest_table = source_table

    factory = SourceDestinationFactory(source_uri, dest_uri)
    source = factory.get_source()
    destination = factory.get_destination()

    m = hashlib.sha256()
    m.update(dest_table.encode("utf-8"))
    pipeline_name = m.hexdigest()
    short_pipeline_name = pipeline_name[:8]

    pipeline = dlt.pipeline(
        pipeline_name=pipeline_name,
        destination=destination.dlt_dest(
            uri=dest_uri,
        ),
        progress=dlt.progress.log(dump_system_stats=False),
        pipelines_dir="pipeline_data",
    )

    print()
    print(f"[bold green]Initiated the pipeline with the following:[/bold green]")
    print(f"[bold yellow]  Pipeline ID:[/bold yellow] {short_pipeline_name}")
    print(
        f"[bold yellow]  Source:[/bold yellow] {factory.source_scheme} / {source_table}"
    )
    print(
        f"[bold yellow]  Destination:[/bold yellow] {factory.destination_scheme} / {dest_table}"
    )
    print(f"[bold yellow]  Incremental Strategy:[/bold yellow] {incremental_strategy}")
    print(
        f"[bold yellow]  Incremental Key:[/bold yellow] {incremental_key if incremental_key else 'None'}"
    )
    print()

    continuePipeline = typer.confirm("Are you sure you would like to continue?")
    if not continuePipeline:
        raise typer.Abort()

    print()
    print(f"[bold green]Starting the ingestion...[/bold green]")
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

    elapsedHuman = ""
    if run_info.started_at:
        elapsed = run_info.finished_at - run_info.started_at
        elapsedHuman = f"in {humanize.precisedelta(elapsed)}"

    print(
        f"[bold green]Successfully finished loading data from '{factory.source_scheme}' to '{factory.destination_scheme}' {elapsedHuman} [/bold green]"
    )

    # printLoadInfo(short_pipeline_name, run_info)


def printLoadInfo(short_pipeline_name: str, info: LoadInfo):
    if info.started_at:
        elapsed = info.finished_at - info.started_at
        print(
            f"  ├── Pipeline {short_pipeline_name} load step completed in [bold green]{humanize.precisedelta(elapsed)}[/bold green]"
        )

    connector = "└──"
    if info.staging_name:
        connector = "├──"

    print(
        f"  {connector} {len(info.loads_ids)} load package{'s were' if len(info.loads_ids) > 1 else ' was'} loaded to destination [bold cyan]{info.destination_name}[/bold cyan] and into dataset [bold cyan]{info.dataset_name}[/bold cyan]",
        highlight=False,
    )
    if info.staging_name:
        print(
            f"  └── The [bold cyan]{info.staging_name}[/bold cyan] staging destination used [bold cyan]{info.staging_displayable_credentials}[/bold cyan] location to stage data"
        )


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
