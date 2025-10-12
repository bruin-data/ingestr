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
    create_nested_resource,
    create_replace_resource,
)
from ingestr.src.mailchimp.settings import (
    MERGE_ENDPOINTS,
    NESTED_ENDPOINTS,
    REPLACE_ENDPOINTS,
)


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
    for replace_endpoint in REPLACE_ENDPOINTS:
        resource_name, endpoint_path, data_key, pk = replace_endpoint
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

    # Create nested resources (depend on parent resources)
    for nested_endpoint in NESTED_ENDPOINTS:
        (
            parent_name,
            parent_path,
            parent_key,
            parent_id_field,
            nested_name,
            nested_path,
            nested_key,
            pk,
        ) = nested_endpoint
        resources.append(
            create_nested_resource(
                base_url,
                session,
                auth,
                parent_name,
                parent_path,
                parent_key,
                parent_id_field,
                nested_name,
                nested_path,
                nested_key,
                pk,
            )
        )

    return tuple(resources)
