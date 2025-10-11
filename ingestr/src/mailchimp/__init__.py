"""
Mailchimp source for data extraction via REST API.

This source provides access to Mailchimp account data.
"""

from typing import Any, Iterator

import dlt
from dlt.sources import DltResource

from ingestr.src.http_client import create_client


@dlt.source(max_table_nesting=0, name="mailchimp_source")
def mailchimp_source(
    api_key: str,
    server: str,
) -> DltResource:
    """
    Mailchimp data source.

    Args:
        api_key: Mailchimp API key for authentication
        server: Server prefix (e.g., 'us10')

    Yields:
        DltResource: Data resource for account information
    """
    base_url = f"https://{server}.api.mailchimp.com/3.0"
    session = create_client()

    @dlt.resource(
        name="account",
        write_disposition="replace",
    )
    def fetch_account() -> Iterator[dict[str, Any]]:
        """
        Fetch account information from Mailchimp.

        Table format: account (no parameters needed)
        """
        response = session.get(
            f"{base_url}/",
            auth=("anystring", api_key),
        )
        response.raise_for_status()
        data = response.json()
        yield data

    return fetch_account
