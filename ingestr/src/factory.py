from typing import Dict, Protocol, Type
from urllib.parse import urlparse

from dlt.common.destination import Destination

from ingestr.src.destinations import (
    AthenaDestination,
    BigQueryDestination,
    ClickhouseDestination,
    CsvDestination,
    DatabricksDestination,
    DuckDBDestination,
    MsSQLDestination,
    PostgresDestination,
    RedshiftDestination,
    S3Destination,
    SnowflakeDestination,
    SynapseDestination,
)
from ingestr.src.sources import (
    AdjustSource,
    AirtableSource,
    AppleAppStoreSource,
    ApplovinMaxSource,
    AppLovinSource,
    AppsflyerSource,
    ArrowMemoryMappedSource,
    AsanaSource,
    AttioSource,
    ChessSource,
    DynamoDBSource,
    ElasticsearchSource,
    FacebookAdsSource,
    FrankfurterSource,
    FreshdeskSource,
    GCSSource,
    GitHubSource,
    GoogleAdsSource,
    GoogleAnalyticsSource,
    GoogleSheetsSource,
    GorgiasSource,
    HubspotSource,
    KafkaSource,
    KinesisSource,
    KlaviyoSource,
    LinkedInAdsSource,
    LocalCsvSource,
    MongoDbSource,
    NotionSource,
    PersonioSource,
    PhantombusterSource,
    PipedriveSource,
    S3Source,
    SalesforceSource,
    ShopifySource,
    SlackSource,
    SolidgateSource,
    SqlSource,
    StripeAnalyticsSource,
    TikTokSource,
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
    "clickhouse",
    "databricks",
    "db2",
    "spanner",
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
    sources: Dict[str, Type[SourceProtocol]] = {
        "csv": LocalCsvSource,
        "mongodb": MongoDbSource,
        "mongodb+srv": MongoDbSource,
        "notion": NotionSource,
        "gsheets": GoogleSheetsSource,
        "shopify": ShopifySource,
        "gorgias": GorgiasSource,
        "github": GitHubSource,
        "chess": ChessSource,
        "stripe": StripeAnalyticsSource,
        "facebookads": FacebookAdsSource,
        "slack": SlackSource,
        "hubspot": HubspotSource,
        "airtable": AirtableSource,
        "klaviyo": KlaviyoSource,
        "appsflyer": AppsflyerSource,
        "kafka": KafkaSource,
        "adjust": AdjustSource,
        "zendesk": ZendeskSource,
        "mmap": ArrowMemoryMappedSource,
        "s3": S3Source,
        "dynamodb": DynamoDBSource,
        "asana": AsanaSource,
        "tiktok": TikTokSource,
        "googleanalytics": GoogleAnalyticsSource,
        "googleads": GoogleAdsSource,
        "appstore": AppleAppStoreSource,
        "gs": GCSSource,
        "linkedinads": LinkedInAdsSource,
        "applovin": AppLovinSource,
        "applovinmax": ApplovinMaxSource,
        "salesforce": SalesforceSource,
        "personio": PersonioSource,
        "kinesis": KinesisSource,
        "pipedrive": PipedriveSource,
        "frankfurter": FrankfurterSource,
        "freshdesk": FreshdeskSource,
        "phantombuster": PhantombusterSource,
        "elasticsearch": ElasticsearchSource,
        "attio": AttioSource,
        "solidgate": SolidgateSource,
    }
    destinations: Dict[str, Type[DestinationProtocol]] = {
        "bigquery": BigQueryDestination,
        "databricks": DatabricksDestination,
        "duckdb": DuckDBDestination,
        "mssql": MsSQLDestination,
        "postgres": PostgresDestination,
        "postgresql": PostgresDestination,
        "postgresql+psycopg2": PostgresDestination,
        "redshift": RedshiftDestination,
        "redshift+psycopg2": RedshiftDestination,
        "redshift+redshift_connector": RedshiftDestination,
        "snowflake": SnowflakeDestination,
        "synapse": SynapseDestination,
        "csv": CsvDestination,
        "athena": AthenaDestination,
        "clickhouse+native": ClickhouseDestination,
        "clickhouse": ClickhouseDestination,
        "s3": S3Destination,
    }

    def __init__(self, source_uri: str, destination_uri: str):
        self.source_uri = source_uri
        source_fields = urlparse(source_uri)
        self.source_scheme = source_fields.scheme

        self.destination_uri = destination_uri
        self.destination_scheme = parse_scheme_from_uri(destination_uri)

    def get_source(self) -> SourceProtocol:
        if self.source_scheme in SQL_SOURCE_SCHEMES:
            return SqlSource()
        elif self.source_scheme in self.sources:
            return self.sources[self.source_scheme]()
        else:
            raise ValueError(f"Unsupported source scheme: {self.source_scheme}")

    def get_destination(self) -> DestinationProtocol:
        if self.destination_scheme in self.destinations:
            return self.destinations[self.destination_scheme]()
        else:
            raise ValueError(
                f"Unsupported destination scheme: {self.destination_scheme}"
            )
