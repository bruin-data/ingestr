from typing import Protocol
from urllib.parse import urlparse

from ingestr.src.destinations import (
    BigQueryDestination,
    DuckDBDestination,
    MsSQLDestination,
    PostgresDestination,
    RedshiftDestination,
    SnowflakeDestination,
)
from ingestr.src.sources import SqlSource

SQL_SOURCE_SCHEMES = [
    "bigquery",
    "duckdb",
    "mssql",
    "mysql",
    "postgres",
    "postgresql",
    "redshift",
    "snowflake",
    "sqlite",
]


class SourceProtocol(Protocol):
    def dlt_source(self, uri: str, table: str, **kwargs):
        pass


class DestinationProtocol(Protocol):
    def dlt_dest(self, uri: str, **kwargs):
        pass

    def dlt_run_params(self, uri: str, table: str, **kwargs) -> dict:
        pass


class SourceDestinationFactory:
    source_scheme: str
    destination_scheme: str

    def __init__(self, source_uri: str, destination_uri: str):
        self.source_uri = source_uri
        source_fields = urlparse(source_uri)
        self.source_scheme = source_fields.scheme

        self.destination_uri = destination_uri
        dest_fields = urlparse(destination_uri)
        self.destination_scheme = dest_fields.scheme

    def get_source(self) -> SourceProtocol:
        if self.source_scheme in SQL_SOURCE_SCHEMES:
            return SqlSource()
        else:
            raise ValueError(f"Unsupported source scheme: {self.source_scheme}")

    def get_destination(self) -> DestinationProtocol:
        match: dict[str, DestinationProtocol] = {
            "bigquery": BigQueryDestination(),
            "postgres": PostgresDestination(),
            "postgresql": PostgresDestination(),
            "snowflake": SnowflakeDestination(),
            "redshift": RedshiftDestination(),
            "duckdb": DuckDBDestination(),
            "mssql": MsSQLDestination(),
        }

        if self.destination_scheme in match:
            return match[self.destination_scheme]
        else:
            raise ValueError(
                f"Unsupported destination scheme: {self.destination_scheme}"
            )
