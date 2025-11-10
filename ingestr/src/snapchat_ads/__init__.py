"""Loads organizations and other data from Snapchat Marketing API"""

from typing import Iterator

import dlt
from dlt.common.typing import TDataItems
from dlt.sources import DltResource

from .snapchat_helpers import SnapchatAdsAPI, fetch_snapchat_data

BASE_URL = "https://adsapi.snapchat.com/v1"


@dlt.source(name="snapchat_ads", max_table_nesting=0)
def snapchat_ads_source(
    refresh_token: str = dlt.secrets.value,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
    organization_id: str = dlt.config.value,
) -> DltResource:
    """Returns a list of resources to load data from Snapchat Marketing API.

    Args:
        refresh_token (str): OAuth refresh token for Snapchat Marketing API
        client_id (str): OAuth client ID
        client_secret (str): OAuth client secret
        organization_id (str): Organization ID (optional for organizations table, required for others)

    Returns:
        DltResource: organizations
    """
    api = SnapchatAdsAPI(
        refresh_token=refresh_token, client_id=client_id, client_secret=client_secret
    )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def organizations(
        updated_at=dlt.sources.incremental("updated_at")
    ) -> Iterator[TDataItems]:
        """Fetch all organizations for the authenticated user."""
        url = f"{BASE_URL}/me/organizations"
        yield from fetch_snapchat_data(api, url, "organizations", "organization")

    @dlt.resource(primary_key="id", write_disposition="merge")
    def fundingsources(
        updated_at=dlt.sources.incremental("updated_at")
    ) -> Iterator[TDataItems]:
        """Fetch all funding sources for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for fundingsources")

        url = f"{BASE_URL}/organizations/{organization_id}/fundingsources"
        yield from fetch_snapchat_data(api, url, "fundingsources", "fundingsource")

    @dlt.resource(primary_key="id", write_disposition="merge")
    def billingcenters(
        updated_at=dlt.sources.incremental("updated_at")
    ) -> Iterator[TDataItems]:
        """Fetch all billing centers for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for billingcenters")

        url = f"{BASE_URL}/organizations/{organization_id}/billingcenters"
        yield from fetch_snapchat_data(api, url, "billingcenters", "billingcenter")

    return organizations, fundingsources, billingcenters
