"""Loads organizations and other data from Snapchat Marketing API"""

from typing import Iterator

import dlt
from dlt.common.typing import TDataItems
from dlt.sources import DltResource

from .snapchat_helpers import SnapchatAdsAPI, create_client

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
        client = create_client()
        headers = api.get_headers()

        url = f"{BASE_URL}/me/organizations"
        response = client.get(url, headers=headers)

        if response.status_code != 200:
            raise ValueError(
                f"Failed to fetch organizations: {response.status_code} - {response.text}"
            )

        result = response.json()

        if result.get("request_status", "").upper() != "SUCCESS":
            raise ValueError(
                f"Request failed: {result.get('request_status')} - {result}"
            )

        organizations_data = result.get("organizations", [])

        for org_item in organizations_data:
            if org_item.get("sub_request_status", "").upper() == "SUCCESS":
                org = org_item.get("organization", {})
                if org:
                    yield org

    @dlt.resource(primary_key="id", write_disposition="merge")
    def fundingsources(
        updated_at=dlt.sources.incremental("updated_at")
    ) -> Iterator[TDataItems]:
        """Fetch all funding sources for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for fundingsources")

        client = create_client()
        headers = api.get_headers()

        url = f"{BASE_URL}/organizations/{organization_id}/fundingsources"
        response = client.get(url, headers=headers)

        if response.status_code != 200:
            raise ValueError(
                f"Failed to fetch funding sources: {response.status_code} - {response.text}"
            )

        result = response.json()

        if result.get("request_status", "").upper() != "SUCCESS":
            raise ValueError(
                f"Request failed: {result.get('request_status')} - {result}"
            )

        fundingsources_data = result.get("fundingsources", [])

        for fs_item in fundingsources_data:
            if fs_item.get("sub_request_status", "").upper() == "SUCCESS":
                fs = fs_item.get("fundingsource", {})
                if fs:
                    yield fs

    return organizations, fundingsources
