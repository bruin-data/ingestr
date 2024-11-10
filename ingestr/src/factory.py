from typing import Protocol
from urllib.parse import urlparse

from dlt.common.destination import Destination

from ingestr.src.destinations import (
    BigQueryDestination,
    CsvDestination,
    DatabricksDestination,
    DuckDBDestination,
    MsSQLDestination,
    PostgresDestination,
    RedshiftDestination,
    SnowflakeDestination,
    SynapseDestination,
)
from ingestr.src.sources import (
    AdjustSource,
    AirtableSource,
    AppsflyerSource,
    ArrowMemoryMappedSource,
    ChessSource,
    FacebookAdsSource,
    GoogleSheetsSource,
    GorgiasSource,
    HubspotSource,
    KafkaSource,
    KlaviyoSource,
    LocalCsvSource,
    MongoDbSource,
    NotionSource,
    S3Source,
    ShopifySource,
    SlackSource,
    SqlSource,
    StripeAnalyticsSource,
    ZendeskSource,
)

SQL_SOURCE_SCHEMES = [
    "bigquery",
    "duckdb",
    "mssql",
    "mysql",
    "mysql+pymysql",
    "mysql+mysqlconnector",
    "postgres",
    "postgresql",
    "postgresql+psycopg2",
    "redshift",
    "redshift+psycopg2",
    "snowflake",
    "sqlite",
    "oracle",
    "oracle+cx_oracle",
    "hana",
]


class SourceProtocol(Protocol):
    def dlt_source(self, uri: str, table: str, **kwargs):
        pass

    def handles_incrementality(self) -> bool:
        pass


class DestinationProtocol(Protocol):
    def dlt_dest(self, uri: str, **kwargs) -> Destination:
        pass

    def dlt_run_params(self, uri: str, table: str, **kwargs):
        pass

    def post_load(self) -> None:
        pass


def parse_scheme_from_uri(uri: str) -> str:
    parsed = urlparse(uri)
    if parsed.scheme != "":
        return parsed.scheme

    uri_parts = uri.split("://")
    if len(uri_parts) > 1:
        return uri_parts[0]

    raise ValueError(f"Could not parse scheme from uri: {uri}")


class SourceDestinationFactory:
    source_scheme: str
    destination_scheme: str

    def __init__(self, source_uri: str, destination_uri: str):
        self.source_uri = source_uri
        source_fields = urlparse(source_uri)
        self.source_scheme = source_fields.scheme

        self.destination_uri = destination_uri
        self.destination_scheme = parse_scheme_from_uri(destination_uri)

    def get_source(self) -> SourceProtocol:
        if self.source_scheme in SQL_SOURCE_SCHEMES:
            return SqlSource()
        elif self.source_scheme == "csv":
            return LocalCsvSource()
        elif self.source_scheme == "mongodb":
            return MongoDbSource()
        elif self.source_scheme == "notion":
            return NotionSource()
        elif self.source_scheme == "gsheets":
            return GoogleSheetsSource()
        elif self.source_scheme == "shopify":
            return ShopifySource()
        elif self.source_scheme == "gorgias":
            return GorgiasSource()
        elif self.source_scheme == "chess":
            return ChessSource()
        elif self.source_scheme == "stripe":
            return StripeAnalyticsSource()
        elif self.source_scheme == "facebookads":
            return FacebookAdsSource()
        elif self.source_scheme == "slack":
            return SlackSource()
        elif self.source_scheme == "hubspot":
            return HubspotSource()
        elif self.source_scheme == "airtable":
            return AirtableSource()
        elif self.source_scheme == "klaviyo":
            return KlaviyoSource()
        elif self.source_scheme == "appsflyer":
            return AppsflyerSource()
        elif self.source_scheme == "kafka":
            return KafkaSource()
        elif self.source_scheme == "adjust":
            return AdjustSource()
        elif self.source_scheme == "zendesk":
            return ZendeskSource()
        elif self.source_scheme == "mmap":
            return ArrowMemoryMappedSource()
        elif self.source_scheme == "s3":
            return S3Source()
        else:
            raise ValueError(f"Unsupported source scheme: {self.source_scheme}")

    def get_destination(self) -> DestinationProtocol:
        match: dict[str, DestinationProtocol] = {
            "bigquery": BigQueryDestination(),
            "databricks": DatabricksDestination(),
            "duckdb": DuckDBDestination(),
            "mssql": MsSQLDestination(),
            "postgres": PostgresDestination(),
            "postgresql": PostgresDestination(),
            "postgresql+psycopg2": PostgresDestination(),
            "redshift": RedshiftDestination(),
            "redshift+psycopg2": RedshiftDestination(),
            "redshift+redshift_connector": RedshiftDestination(),
            "snowflake": SnowflakeDestination(),
            "synapse": SynapseDestination(),
            "csv": CsvDestination(),
        }

        if self.destination_scheme in match:
            return match[self.destination_scheme]
        else:
            raise ValueError(
                f"Unsupported destination scheme: {self.destination_scheme}"
            )
