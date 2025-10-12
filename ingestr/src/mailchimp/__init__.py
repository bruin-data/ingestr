"""
Mailchimp source for data extraction via REST API.

This source provides access to Mailchimp account data.
"""

from typing import Any, Iterable, Iterator

import dlt
from dlt.sources import DltResource

from ingestr.src.http_client import create_client
from ingestr.src.mailchimp.helpers import (
    create_merge_resource,
    create_replace_resource,
)

# Endpoints with merge disposition (have both primary_key and incremental_key)
MERGE_ENDPOINTS = [
    ("audiences", "lists", "lists", "id", "date_created"),
    ("automations", "automations", "automations", "id", "create_time"),
    ("campaigns", "campaigns", "campaigns", "id", "create_time"),
    ("connected_sites", "connected-sites", "sites", "id", "updated_at"),
    ("conversations", "conversations", "conversations", "id", "last_message.timestamp"),
    ("ecommerce_stores", "ecommerce/stores", "stores", "id", "updated_at"),
    ("facebook_ads", "facebook-ads", "facebook_ads", "id", "updated_at"),
    ("landing_pages", "landing-pages", "landing_pages", "id", "updated_at"),
    ("reports", "reports", "reports", "id", "send_time"),
]

# Endpoints with replace disposition
REPLACE_ENDPOINTS: list[tuple[str, str, str, str | None]] = [
    ("account_exports", "account-exports", "exports", None),
    ("authorized_apps", "authorized-apps", "apps", "id"),
    ("batches", "batches", "batches", None),
    ("campaign_folders", "campaign-folders", "folders", "id"),
    ("chimp_chatter", "activity-feed/chimp-chatter", "chimp_chatter", None),
]


@dlt.source(max_table_nesting=0, name="mailchimp_source")
def mailchimp_source(
    api_key: str,
    server: str,
) -> Iterable[DltResource]:
    """
    Mailchimp data source.

    Args:
        api_key: Mailchimp API key for authentication
        server: Server prefix (e.g., 'us10')

    Yields:
        DltResource: Data resources for Mailchimp data
    """
    base_url = f"https://{server}.api.mailchimp.com/3.0"
    session = create_client()
    auth = ("anystring", api_key)

    @dlt.resource(
        name="account",
        write_disposition="replace",
    )
    def fetch_account() -> Iterator[dict[str, Any]]:
        """
        Fetch account information from Mailchimp.

        Table format: account (no parameters needed)
        """
        response = session.get(f"{base_url}/", auth=auth)
        response.raise_for_status()
        data = response.json()
        yield data

    # Create resources dynamically
    resources = [fetch_account]

    # Create merge resources (with incremental loading)
    for (
        resource_name,
        endpoint_path,
        data_key,
        primary_key,
        incremental_key,
    ) in MERGE_ENDPOINTS:
        resources.append(
            create_merge_resource(
                base_url,
                session,
                auth,
                resource_name,
                endpoint_path,
                data_key,
                primary_key,
                incremental_key,
            )
        )

    # Create replace resources (without incremental loading)
    for endpoint in REPLACE_ENDPOINTS:
        resource_name, endpoint_path, data_key, pk = endpoint
        resources.append(
            create_replace_resource(
                base_url,
                session,
                auth,
                resource_name,
                endpoint_path,
                data_key,
                pk,
            )
        )

    return tuple(resources)
