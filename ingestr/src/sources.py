import base64
import csv
import json
from datetime import date
from typing import Any, Callable, Optional
from urllib.parse import parse_qs, urlparse

import dlt
import pendulum
from dlt.common.configuration.specs import AwsCredentials
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TSecretStrValue

from ingestr.src.adjust import REQUIRED_CUSTOM_DIMENSIONS, adjust_source
from ingestr.src.adjust.adjust_helpers import parse_filters
from ingestr.src.airtable import airtable_source
from ingestr.src.appsflyer._init_ import appsflyer_source
from ingestr.src.arrow import memory_mapped_arrow
from ingestr.src.chess import source
from ingestr.src.facebook_ads import facebook_ads_source, facebook_insights_source
from ingestr.src.filesystem import readers
from ingestr.src.google_sheets import google_spreadsheet
from ingestr.src.gorgias import gorgias_source
from ingestr.src.hubspot import hubspot
from ingestr.src.kafka import kafka_consumer
from ingestr.src.kafka.helpers import KafkaCredentials
from ingestr.src.klaviyo._init_ import klaviyo_source
from ingestr.src.mongodb import mongodb_collection
from ingestr.src.notion import notion_databases
from ingestr.src.shopify import shopify_source
from ingestr.src.slack import slack_source
from ingestr.src.sql_database import sql_table
from ingestr.src.stripe_analytics import stripe_source
from ingestr.src.table_definition import table_string_to_dataclass
from ingestr.src.zendesk import zendesk_chat, zendesk_support, zendesk_talk
from ingestr.src.zendesk.helpers.credentials import (
    ZendeskCredentialsOAuth,
    ZendeskCredentialsToken,
)


class SqlSource:
    table_builder: Callable

    def __init__(self, table_builder=sql_table) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        table_fields = table_string_to_dataclass(table)

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                # primary_key=(),
                initial_value=start_value,
                end_value=end_value,
            )

        if uri.startswith("mysql://"):
            uri = uri.replace("mysql://", "mysql+pymysql://")

        table_instance = self.table_builder(
            credentials=uri,
            schema=table_fields.dataset,
            table=table_fields.table,
            incremental=incremental,
            merge_key=kwargs.get("merge_key"),
            backend=kwargs.get("sql_backend", "sqlalchemy"),
            chunk_size=kwargs.get("page_size", None),
        )

        return table_instance


class ArrowMemoryMappedSource:
    table_builder: Callable

    def __init__(self, table_builder=memory_mapped_arrow) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        import os

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
            )

        file_path = uri.split("://")[1]
        if not os.path.exists(file_path):
            raise ValueError(f"File at path {file_path} does not exist")

        if os.path.isdir(file_path):
            raise ValueError(
                f"Path {file_path} is a directory, it should be an Arrow memory mapped file"
            )

        primary_key = kwargs.get("primary_key")
        merge_key = kwargs.get("merge_key")

        table_instance = self.table_builder(
            path=file_path,
            incremental=incremental,
            merge_key=merge_key,
            primary_key=primary_key,
        )

        return table_instance


class MongoDbSource:
    table_builder: Callable

    def __init__(self, table_builder=mongodb_collection) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        table_fields = table_string_to_dataclass(table)

        incremental = None
        if kwargs.get("incremental_key"):
            start_value = kwargs.get("interval_start")
            end_value = kwargs.get("interval_end")

            incremental = dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                initial_value=start_value,
                end_value=end_value,
            )

        table_instance = self.table_builder(
            connection_url=uri,
            database=table_fields.dataset,
            collection=table_fields.table,
            parallel=True,
            incremental=incremental,
        )

        return table_instance


class LocalCsvSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        def csv_file(
            incremental: Optional[dlt.sources.incremental[Any]] = None,
        ):
            file_path = uri.split("://")[1]
            myFile = open(file_path, "r")
            reader = csv.DictReader(myFile)
            if not reader.fieldnames:
                raise RuntimeError(
                    "failed to extract headers from the CSV, are you sure the given file contains a header row?"
                )

            incremental_key = kwargs.get("incremental_key")
            if incremental_key and incremental_key not in reader.fieldnames:
                raise ValueError(
                    f"incremental_key '{incremental_key}' not found in the CSV file"
                )

            page_size = 1000
            page = []
            current_items = 0
            for dictionary in reader:
                if current_items < page_size:
                    if incremental_key and incremental and incremental.start_value:
                        inc_value = dictionary.get(incremental_key)
                        if inc_value is None:
                            raise ValueError(
                                f"incremental_key '{incremental_key}' not found in the CSV file"
                            )

                        if inc_value < incremental.start_value:
                            continue

                    page.append(dictionary)
                    current_items += 1
                else:
                    yield page
                    page = []
                    current_items = 0

            if page:
                yield page

        return dlt.resource(
            csv_file,
            merge_key=kwargs.get("merge_key"),  # type: ignore
        )(
            incremental=dlt.sources.incremental(
                kwargs.get("incremental_key", ""),
                initial_value=kwargs.get("interval_start"),
                end_value=kwargs.get("interval_end"),
            )
        )


class NotionSource:
    table_builder: Callable

    def __init__(self, table_builder=notion_databases) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Notion")

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")
        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Notion")

        return self.table_builder(
            database_ids=[{"id": table}],
            api_key=api_key[0],
        )


class ShopifySource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")
        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Shopify")

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        resource = None
        if table in [
            "products",
            "products_legacy",
            "orders",
            "customers",
            "inventory_items",
            "transactions",
            "balance",
            "events",
            "price_rules",
            "discounts",
            "taxonomy",
        ]:
            resource = table
        else:
            raise ValueError(
                f"Table name '{table}' is not supported for Shopify source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        return shopify_source(
            private_app_password=api_key[0],
            shop_url=f"https://{source_fields.netloc}",
            **date_args,
        ).with_resources(resource)


class GorgiasSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Gorgias takes care of incrementality on its own, you should not provide incremental_key"
            )

        # gorgias://domain?api_key=<api_key>&email=<email>

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")
        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Gorgias")

        email = source_params.get("email")
        if not email:
            raise ValueError("email in the URI is required to connect to Gorgias")

        resource = None
        if table in ["customers", "tickets", "ticket_messages", "satisfaction_surveys"]:
            resource = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Gorgias source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        return gorgias_source(
            domain=source_fields.netloc,
            email=email[0],
            api_key=api_key[0],
            **date_args,
        ).with_resources(resource)


class GoogleSheetsSource:
    table_builder: Callable

    def __init__(self, table_builder=google_spreadsheet) -> None:
        self.table_builder = table_builder

    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Google Sheets")

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)

        cred_path = source_params.get("credentials_path")
        credentials_base64 = source_params.get("credentials_base64")
        if not cred_path and not credentials_base64:
            raise ValueError(
                "credentials_path or credentials_base64 is required in the URI to get data from Google Sheets"
            )

        credentials = {}
        if cred_path:
            with open(cred_path[0], "r") as f:
                credentials = json.load(f)
        elif credentials_base64:
            credentials = json.loads(
                base64.b64decode(credentials_base64[0]).decode("utf-8")
            )

        table_fields = table_string_to_dataclass(table)
        return self.table_builder(
            credentials=credentials,
            spreadsheet_url_or_id=table_fields.dataset,
            range_names=[table_fields.table],
            get_named_ranges=False,
        )


class ChessSource:
    def handles_incrementality(self) -> bool:
        return True

    # chess://?players=john,peter
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Chess takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        list_players = None
        if "players" in source_params:
            list_players = source_params["players"][0].split(",")
        else:
            list_players = [
                "MagnusCarlsen",
                "HikaruNakamura",
                "ArjunErigaisi",
                "IanNepomniachtchi",
            ]

        date_args = {}
        start_date = kwargs.get("interval_start")
        end_date = kwargs.get("interval_end")
        if start_date and end_date:
            if isinstance(start_date, date) and isinstance(end_date, date):
                date_args["start_month"] = start_date.strftime("%Y/%m")
                date_args["end_month"] = end_date.strftime("%Y/%m")

        table_mapping = {
            "profiles": "players_profiles",
            "games": "players_games",
            "archives": "players_archives",
        }

        if table not in table_mapping:
            raise ValueError(
                f"Resource '{table}' is not supported for Chess source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        return source(players=list_players, **date_args).with_resources(
            table_mapping[table]
        )


class StripeAnalyticsSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Stripe takes care of incrementality on its own, you should not provide incremental_key"
            )

        api_key = None
        source_field = urlparse(uri)
        source_params = parse_qs(source_field.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Stripe")

        endpoint = None
        table = str.capitalize(table)

        if table in [
            "Subscription",
            "Account",
            "Coupon",
            "Customer",
            "Product",
            "Price",
            "BalanceTransaction",
            "Invoice",
            "Event",
        ]:
            endpoint = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for stripe source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        return stripe_source(
            endpoints=[
                endpoint,
            ],
            stripe_secret_key=api_key[0],
            **date_args,
        ).with_resources(endpoint)


class FacebookAdsSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        # facebook_ads://?access_token=abcd&account_id=1234
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Facebook Ads takes care of incrementality on its own, you should not provide incremental_key"
            )

        access_token = None
        account_id = None
        source_field = urlparse(uri)
        source_params = parse_qs(source_field.query)
        access_token = source_params.get("access_token")
        account_id = source_params.get("account_id")

        if not access_token or not account_id:
            raise ValueError(
                "access_token and accound_id are required to connect to Facebook Ads."
            )

        endpoint = None
        if table in ["campaigns", "ad_sets", "ad_creatives", "ads", "leads"]:
            endpoint = table
        elif table in "facebook_insights":
            return facebook_insights_source(
                access_token=access_token[0],
                account_id=account_id[0],
            ).with_resources("facebook_insights")
        else:
            raise ValueError(
                "fResource '{table}' is not supported for Facebook Ads source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        return facebook_ads_source(
            access_token=access_token[0],
            account_id=account_id[0],
        ).with_resources(endpoint)


class SlackSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Slack takes care of incrementality on its own, you should not provide incremental_key"
            )
        # slack://?api_key=<apikey>
        api_key = None
        source_field = urlparse(uri)
        source_query = parse_qs(source_field.query)
        api_key = source_query.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Slack")

        endpoint = None
        msg_channels = None
        if table in ["channels", "users", "access_logs"]:
            endpoint = table
        elif table.startswith("messages"):
            channels_part = table.split(":")[1]
            msg_channels = channels_part.split(",")
            endpoint = "messages"
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for slack source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        date_args = {}
        if kwargs.get("interval_start"):
            date_args["start_date"] = kwargs.get("interval_start")

        if kwargs.get("interval_end"):
            date_args["end_date"] = kwargs.get("interval_end")

        return slack_source(
            access_token=api_key[0],
            table_per_channel=False,
            selected_channels=msg_channels,
            **date_args,
        ).with_resources(endpoint)


class HubspotSource:
    def handles_incrementality(self) -> bool:
        return True

    # hubspot://?api_key=<api_key>
    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Hubspot takes care of incrementality on its own, you should not provide incremental_key"
            )

        api_key = None
        source_parts = urlparse(uri)
        source_parmas = parse_qs(source_parts.query)
        api_key = source_parmas.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Hubspot")

        endpoint = None
        if table in ["contacts", "companies", "deals", "tickets", "products", "quotes"]:
            endpoint = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Hubspot source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        return hubspot(
            api_key=api_key[0],
        ).with_resources(endpoint)


class AirtableSource:
    def handles_incrementality(self) -> bool:
        return True

    # airtable://?access_token=<access_token>&base_id=<base_id>

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError("Incremental loads are not supported for Airtable")

        if not table:
            raise ValueError("Source table is required to connect to Airtable")

        tables = table.split(",")

        source_parts = urlparse(uri)
        source_fields = parse_qs(source_parts.query)
        base_id = source_fields.get("base_id")
        access_token = source_fields.get("access_token")

        if not base_id or not access_token:
            raise ValueError(
                "base_id and access_token in the URI are required to connect to Airtable"
            )

        return airtable_source(
            base_id=base_id[0], table_names=tables, access_token=access_token[0]
        )


class KlaviyoSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "klaviyo_source takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to klaviyo")

        resource = None
        if table in [
            "events",
            "profiles",
            "campaigns",
            "metrics",
            "tags",
            "coupons",
            "catalog-variants",
            "catalog-categories",
            "catalog-items",
            "forms",
            "lists",
            "images",
            "segments",
            "flows",
            "templates",
        ]:
            resource = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Klaviyo source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        start_date = kwargs.get("interval_start") or "2000-01-01"
        return klaviyo_source(
            api_key=api_key[0],
            start_date=start_date,
        ).with_resources(resource)


class KafkaSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        # kafka://?bootstrap_servers=localhost:9092&group_id=test_group&security_protocol=SASL_SSL&sasl_mechanisms=PLAIN&sasl_username=example_username&sasl_password=example_secret
        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)

        bootstrap_servers = source_params.get("bootstrap_servers")
        group_id = source_params.get("group_id")
        security_protocol = source_params.get("security_protocol", [])
        sasl_mechanisms = source_params.get("sasl_mechanisms", [])
        sasl_username = source_params.get("sasl_username", [])
        sasl_password = source_params.get("sasl_password", [])
        batch_size = source_params.get("batch_size", [3000])
        batch_timeout = source_params.get("batch_timeout", [3])

        if not bootstrap_servers:
            raise ValueError(
                "bootstrap_servers in the URI is required to connect to kafka"
            )

        if not group_id:
            raise ValueError("group_id in the URI is required to connect to kafka")

        start_date = kwargs.get("interval_start")
        return kafka_consumer(
            topics=[table],
            credentials=KafkaCredentials(
                bootstrap_servers=bootstrap_servers[0],
                group_id=group_id[0],
                security_protocol=(
                    security_protocol[0] if len(security_protocol) > 0 else None
                ),  # type: ignore
                sasl_mechanisms=(
                    sasl_mechanisms[0] if len(sasl_mechanisms) > 0 else None
                ),  # type: ignore
                sasl_username=sasl_username[0] if len(sasl_username) > 0 else None,  # type: ignore
                sasl_password=sasl_password[0] if len(sasl_password) > 0 else None,  # type: ignore
            ),
            start_from=start_date,
            batch_size=int(batch_size[0]),
            batch_timeout=int(batch_timeout[0]),
        )


class AdjustSource:
    def handles_incrementality(self) -> bool:
        return False

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key") and not table.startswith("custom:"):
            raise ValueError(
                "Adjust takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_part = urlparse(uri)
        source_params = parse_qs(source_part.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Adjust")

        lookback_days = int(source_params.get("lookback_days", [30])[0])

        start_date = (
            pendulum.now()
            .replace(hour=0, minute=0, second=0, microsecond=0)
            .subtract(days=lookback_days)
        )
        if kwargs.get("interval_start"):
            start_date = (
                ensure_pendulum_datetime(str(kwargs.get("interval_start")))
                .replace(hour=0, minute=0, second=0, microsecond=0)
                .subtract(days=lookback_days)
            )

        end_date = pendulum.now()
        if kwargs.get("interval_end"):
            end_date = ensure_pendulum_datetime(str(kwargs.get("interval_end")))

        dimensions = None
        metrics = None
        filters = []
        if table.startswith("custom:"):
            fields = table.split(":")
            if len(fields) != 3 and len(fields) != 4:
                raise ValueError(
                    "Invalid Adjust custom table format. Expected format: custom:<dimensions>,<metrics> or custom:<dimensions>:<metrics>:<filters>"
                )

            dimensions = fields[1].split(",")
            metrics = fields[2].split(",")
            table = "custom"

            found = False
            for dimension in dimensions:
                if dimension in REQUIRED_CUSTOM_DIMENSIONS:
                    found = True
                    break

            if not found:
                raise ValueError(
                    f"At least one of the required dimensions is missing for custom Adjust report: {REQUIRED_CUSTOM_DIMENSIONS}"
                )

            if len(fields) == 4:
                filters_raw = fields[3]
                filters = parse_filters(filters_raw)

        return adjust_source(
            start_date=start_date,
            end_date=end_date,
            api_key=api_key[0],
            dimensions=dimensions,
            metrics=metrics,
            merge_key=kwargs.get("merge_key"),
            filters=filters,
        ).with_resources(table)


class AppsflyerSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Appsflyer_Source takes care of incrementality on its own, you should not provide incremental_key"
            )

        source_fields = urlparse(uri)
        source_params = parse_qs(source_fields.query)
        api_key = source_params.get("api_key")

        if not api_key:
            raise ValueError("api_key in the URI is required to connect to Appsflyer")

        resource = None
        if table in ["campaigns", "creatives"]:
            resource = table
        else:
            raise ValueError(
                f"Resource '{table}' is not supported for Appsflyer source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )

        start_date = kwargs.get("interval_start") or "2024-01-02"
        end_date = kwargs.get("interval_end") or "2024-01-29"

        return appsflyer_source(
            api_key=api_key[0],
            start_date=start_date,
            end_date=end_date,
        ).with_resources(resource)


class ZendeskSource:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "Zendesk takes care of incrementality on its own, you should not provide incremental_key"
            )

        interval_start = kwargs.get("interval_start")
        interval_end = kwargs.get("interval_end")
        start_date = (
            interval_start.strftime("%Y-%m-%d") if interval_start else "2000-01-01"
        )
        end_date = interval_end.strftime("%Y-%m-%d") if interval_end else None

        source_fields = urlparse(uri)
        subdomain = source_fields.hostname
        if not subdomain:
            raise ValueError("Subdomain is required to connect with Zendesk")

        if not source_fields.username and source_fields.password:
            oauth_token = source_fields.password
            if not oauth_token:
                raise ValueError(
                    "oauth_token in the URI is required to connect to Zendesk"
                )
            credentials = ZendeskCredentialsOAuth(
                subdomain=subdomain, oauth_token=oauth_token
            )
        elif source_fields.username and source_fields.password:
            email = source_fields.username
            api_token = source_fields.password
            if not email or not api_token:
                raise ValueError(
                    "Both email and token must be provided to connect to Zendesk"
                )
            credentials = ZendeskCredentialsToken(
                subdomain=subdomain, email=email, token=api_token
            )
        else:
            raise ValueError("Invalid URI format")

        if table in [
            "ticket_metrics",
            "users",
            "ticket_metric_events",
            "ticket_forms",
            "tickets",
            "targets",
            "activities",
            "brands",
            "groups",
            "organizations",
            "sla_policies",
            "automations",
        ]:
            return zendesk_support(
                credentials=credentials, start_date=start_date, end_date=end_date
            ).with_resources(table)
        elif table in [
            "greetings",
            "settings",
            "addresses",
            "legs_incremental",
            "calls",
            "phone_numbers",
            "lines",
            "agents_activity",
        ]:
            return zendesk_talk(
                credentials=credentials, start_date=start_date, end_date=end_date
            ).with_resources(table)
        elif table in ["chats"]:
            return zendesk_chat(
                credentials=credentials, start_date=start_date, end_date=end_date
            ).with_resources(table)
        else:
            raise ValueError(
                "fResource '{table}' is not supported for Zendesk source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
            )


class S3Source:
    def handles_incrementality(self) -> bool:
        return True

    def dlt_source(self, uri: str, table: str, **kwargs):
        if kwargs.get("incremental_key"):
            raise ValueError(
                "S3 takes care of incrementality on its own, you should not provide incremental_key"
            )

        parsed_uri = urlparse(uri)
        source_fields = parse_qs(parsed_uri.query)
        access_key_id = source_fields.get("access_key_id")
        if not access_key_id:
            raise ValueError("access_key_id is required to connect to S3")

        secret_access_key = source_fields.get("secret_access_key")
        if not secret_access_key:
            raise ValueError("secret_access_key is required to connect to S3")

        bucket_name = parsed_uri.hostname
        if not bucket_name:
            raise ValueError(
                "Invalid S3 URI: The bucket name is missing. Ensure your S3 URI follows the format 's3://bucket-name/path/to/file"
            )
        bucket_url = f"s3://{bucket_name}"

        path_to_file = parsed_uri.path.lstrip("/")
        if not path_to_file:
            raise ValueError(
                "Invalid S3 URI: The file path is missing. Ensure your S3 URI follows the format 's3://bucket-name/path/to/file"
            )

        aws_credentials = AwsCredentials(
            aws_access_key_id=access_key_id[0],
            aws_secret_access_key=TSecretStrValue(secret_access_key[0]),
        )

        file_extension = path_to_file.split(".")[-1]
        if file_extension == "csv":
            endpoint = "read_csv"
        elif file_extension == "jsonl":
            endpoint = "read_jsonl"
        elif file_extension == "parquet":
            endpoint = "read_parquet"
        else:
            raise ValueError(
                "S3 Source only supports specific formats files: csv, jsonl, parquet"
            )

        return readers(
            bucket_url=bucket_url, credentials=aws_credentials, file_glob=path_to_file
        ).with_resources(endpoint)
