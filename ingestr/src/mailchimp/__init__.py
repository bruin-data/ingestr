"""
Mailchimp source for data extraction via REST API.

This source provides access to Mailchimp account data.
"""

from typing import Any, Iterable, Iterator

import dlt
from dlt.sources import DltResource

from ingestr.src.http_client import create_client
from ingestr.src.mailchimp.helpers import fetch_paginated


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

    @dlt.resource(
        name="account_exports",
        write_disposition="replace",
    )
    def fetch_account_exports() -> Iterator[dict[str, Any]]:
        """
        Fetch account exports from Mailchimp.

        Table format: account_exports (no parameters needed)
        """
        url = f"{base_url}/account-exports"
        yield from fetch_paginated(session, url, auth, data_key="exports")

    @dlt.resource(
        name="audiences",
        write_disposition="replace",
        primary_key="id",
    )
    def fetch_audiences() -> Iterator[dict[str, Any]]:
        """
        Fetch audiences (lists) from Mailchimp.

        Table format: audiences (no parameters needed)
        """
        url = f"{base_url}/lists"
        yield from fetch_paginated(session, url, auth, data_key="lists")

    @dlt.resource(
        name="authorized_apps",
        write_disposition="replace",
        primary_key="id",
    )
    def fetch_authorized_apps() -> Iterator[dict[str, Any]]:
        """
        Fetch authorized apps from Mailchimp.

        Table format: authorized_apps (no parameters needed)
        """
        url = f"{base_url}/authorized-apps"
        yield from fetch_paginated(session, url, auth, data_key="apps")

    return fetch_account, fetch_account_exports, fetch_audiences, fetch_authorized_apps
