import hashlib
from datetime import datetime
from typing import Optional

import dlt
import humanize
import typer
from dlt.common.runtime.collector import Collector
from rich.console import Console
from rich.status import Status
from typing_extensions import Annotated

from ingestr.src.factory import SourceDestinationFactory
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


class SpinnerCollector(Collector):
    """A Collector that shows progress with `tqdm` progress bars"""

    status: Status
    current_step: str
    started: bool

    def __init__(self) -> None:
        self.status = Status("Ingesting data...", spinner="dots")
        self.started = False

    def update(
        self,
        name: str,
        inc: int = 1,
        total: Optional[int] = None,
        message: Optional[str] = None,
        label: str = "",
    ) -> None:
        self.status.update(self.current_step)

    def _start(self, step: str) -> None:
        self.current_step = self.__step_to_label(step)
        self.status.start()

    def __step_to_label(self, step: str) -> str:
        verb = step.split(" ")[0].lower()
        if verb.startswith("normalize"):
            return "Normalizing the data"
        elif verb.startswith("load"):
            return "Loading the data to the destination"
        elif verb.startswith("extract"):
            return "Extracting the data from the source"

        return f"{verb.capitalize()} the data"

    def _stop(self) -> None:
        self.status.stop()


@app.command()
def ingest(
    source_uri: Annotated[
        str,
        typer.Option(help="The URI of the [green]source[/green]", envvar="SOURCE_URI"),
    ],  # type: ignore
    dest_uri: Annotated[
        str,
        typer.Option(
            help="The URI of the [cyan]destination[/cyan]", envvar="DESTINATION_URI"
        ),
    ],  # type: ignore
    source_table: Annotated[
        str,
        typer.Option(
            help="The table name in the [green]source[/green] to fetch",
            envvar="SOURCE_TABLE",
        ),
    ],  # type: ignore
    dest_table: Annotated[
        str,
        typer.Option(
            help="The table in the [cyan]destination[/cyan] to save the data into",
            envvar="DESTINATION_TABLE",
        ),
    ] = None,  # type: ignore
    incremental_key: Annotated[
        str,
        typer.Option(
            help="The incremental key from the table to be used for incremental strategies",
            envvar="INCREMENTAL_KEY",
        ),
    ] = None,  # type: ignore
    incremental_strategy: Annotated[
        str,
        typer.Option(
            help="The incremental strategy to use, must be one of 'replace', 'append', 'delete+insert', or 'merge'",
            envvar="INCREMENTAL_STRATEGY",
        ),
    ] = "replace",  # type: ignore
    interval_start: Annotated[
        Optional[datetime],
        typer.Option(
            help="The start of the interval the incremental key will cover",
            formats=DATE_FORMATS,
            envvar="INTERVAL_START",
        ),
    ] = None,  # type: ignore
    interval_end: Annotated[
        Optional[datetime],
        typer.Option(
            help="The end of the interval the incremental key will cover",
            formats=DATE_FORMATS,
            envvar="INTERVAL_END",
        ),
    ] = None,  # type: ignore
    primary_key: Annotated[
        Optional[list[str]],
        typer.Option(
            help="The key that will be used to deduplicate the resulting table",
            envvar="PRIMARY_KEY",
        ),
    ] = None,  # type: ignore
    yes: Annotated[
        Optional[bool],
        typer.Option(
            help="Skip the confirmation prompt and ingest right away",
            envvar="SKIP_CONFIRMATION",
        ),
    ] = False,  # type: ignore
    full_refresh: Annotated[
        bool,
        typer.Option(
            help="Ignore the state and refresh the destination table completely",
            envvar="FULL_REFRESH",
        ),
    ] = False,  # type: ignore
):
    track(
        "command_triggered",
        {
            "command": "ingest",
        },
    )

    try:
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

        merge_key = None
        if incremental_strategy == "delete+insert":
            merge_key = incremental_key
            incremental_strategy = "merge"

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
            progress=SpinnerCollector(),
            pipelines_dir="pipeline_data",
            full_refresh=full_refresh,
        )

        print()
        print("[bold green]Initiated the pipeline with the following:[/bold green]")
        print(
            f"[bold yellow]  Source:[/bold yellow] {factory.source_scheme} / {source_table}"
        )
        print(
            f"[bold yellow]  Destination:[/bold yellow] {factory.destination_scheme} / {dest_table}"
        )
        print(
            f"[bold yellow]  Incremental Strategy:[/bold yellow] {incremental_strategy}"
        )
        print(
            f"[bold yellow]  Incremental Key:[/bold yellow] {incremental_key if incremental_key else 'None'}"
        )
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

        run_info = pipeline.run(
            source.dlt_source(
                uri=source_uri,
                table=source_table,
                incremental_key=incremental_key,
                merge_key=merge_key,
                interval_start=interval_start,
                interval_end=interval_end,
            ),
            **destination.dlt_run_params(
                uri=dest_uri,
                table=dest_table,
            ),
            write_disposition=incremental_strategy,  # type: ignore
            primary_key=(primary_key if primary_key and len(primary_key) > 0 else None),  # type: ignore
        )

        destination.post_load()

        elapsedHuman = ""
        if run_info.started_at:
            elapsed = run_info.finished_at - run_info.started_at
            elapsedHuman = f"in {humanize.precisedelta(elapsed)}"

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
