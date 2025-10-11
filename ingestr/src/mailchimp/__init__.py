"""
Mailchimp source for data extraction via REST API.

This source provides access to Mailchimp account data.
"""

from typing import Any, Iterable, Iterator

import dlt
from dlt.sources import DltResource

from ingestr.src.http_client import create_client
from ingestr.src.mailchimp.helpers import fetch_paginated


# Endpoint configurations: (resource_name, endpoint_path, data_key, primary_key)
ENDPOINTS = [
    ("account_exports", "account-exports", "exports", None),
    ("audiences", "lists", "lists", "id"),
    ("authorized_apps", "authorized-apps", "apps", "id"),
    ("automations", "automations", "automations", "id"),
    ("batches", "batches", "batches", None),
    ("campaign_folders", "campaign-folders", "folders", "id"),
    ("campaigns", "campaigns", "campaigns", "id"),
    ("chimp_chatter", "activity-feed/chimp-chatter", "chimp_chatter", None),
    ("connected_sites", "connected-sites", "sites", "id"),
    ("conversations", "conversations", "conversations", "id"),
    ("ecommerce_stores", "ecommerce/stores", "stores", "id"),
    ("facebook_ads", "facebook-ads", "facebook_ads", "id"),
    ("landing_pages", "landing-pages", "landing_pages", "id"),
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

    # Create resources dynamically from ENDPOINTS config
    resources = [fetch_account]

    for resource_name, endpoint_path, data_key, primary_key in ENDPOINTS:

        def make_resource(name, path, key, pk):
            @dlt.resource(
                name=name,
                write_disposition="replace",
                primary_key=pk,
            )
            def fetch_data() -> Iterator[dict[str, Any]]:
                url = f"{base_url}/{path}"
                yield from fetch_paginated(session, url, auth, data_key=key)

            return fetch_data

        resources.append(make_resource(resource_name, endpoint_path, data_key, primary_key))

    return tuple(resources)
