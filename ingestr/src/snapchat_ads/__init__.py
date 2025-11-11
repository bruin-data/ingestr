"""Loads organizations and other data from Snapchat Marketing API"""

from typing import Iterator

import dlt
from dlt.common.typing import TDataItems

from .client import SnapchatAdsAPI, create_client
from .helpers import (
    fetch_account_id_resource,
    fetch_snapchat_data,
    fetch_snapchat_data_with_params,
    fetch_with_paginate_account_id,
    paginate,
)

BASE_URL = "https://adsapi.snapchat.com/v1"


@dlt.source(name="snapchat_ads", max_table_nesting=0)
def snapchat_ads_source(
    refresh_token: str = dlt.secrets.value,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
    organization_id: str | None = None,
    ad_account_id: str | None = None,
    start_date: str | None = None,
    end_date: str | None = None,
):
    """Returns a list of resources to load data from Snapchat Marketing API.

    Args:
        refresh_token (str): OAuth refresh token for Snapchat Marketing API
        client_id (str): OAuth client ID
        client_secret (str): OAuth client secret
        organization_id (str): Organization ID (optional for organizations table, required for others)
        ad_account_id (str): Ad Account ID (optional, used to filter resources by ad account)
        start_date (str): Optional start date for filtering data
        end_date (str): Optional end date for filtering data

    Returns:
        tuple: A tuple of three DltResource objects (organizations, fundingsources, billingcenters)
    """
    api = SnapchatAdsAPI(
        refresh_token=refresh_token, client_id=client_id, client_secret=client_secret
    )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def organizations(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all organizations for the authenticated user."""
        url = f"{BASE_URL}/me/organizations"
        yield from fetch_snapchat_data(
            api, url, "organizations", "organization", start_date, end_date
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def fundingsources(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all funding sources for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for fundingsources")

        url = f"{BASE_URL}/organizations/{organization_id}/fundingsources"
        yield from fetch_snapchat_data(
            api, url, "fundingsources", "fundingsource", start_date, end_date
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def billingcenters(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all billing centers for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for billingcenters")

        url = f"{BASE_URL}/organizations/{organization_id}/billingcenters"
        yield from fetch_snapchat_data(
            api, url, "billingcenters", "billingcenter", start_date, end_date
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def adaccounts(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all ad accounts for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for adaccounts")

        url = f"{BASE_URL}/organizations/{organization_id}/adaccounts"
        yield from fetch_snapchat_data(
            api, url, "adaccounts", "adaccount", start_date, end_date
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def invoices(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all invoices for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch invoices only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get invoices for each.
        """
        # If specific ad_account_id provided, fetch only that account's invoices
        if ad_account_id:
            url = f"{BASE_URL}/adaccounts/{ad_account_id}/invoices"
            yield from fetch_snapchat_data(
                api, url, "invoices", "invoice", start_date, end_date
            )
        else:
            # Otherwise, fetch all ad accounts first
            if not organization_id:
                raise ValueError(
                    "organization_id is required to fetch invoices for all ad accounts"
                )

            accounts_url = f"{BASE_URL}/organizations/{organization_id}/adaccounts"
            accounts_data = list(
                fetch_snapchat_data(
                    api,
                    accounts_url,
                    "adaccounts",
                    "adaccount",
                    start_date,
                    end_date,
                )
            )

            # Then fetch invoices for each ad account
            for account in accounts_data:
                account_id = account.get("id")
                if account_id:
                    invoices_url = f"{BASE_URL}/adaccounts/{account_id}/invoices"
                    yield from fetch_snapchat_data(
                        api,
                        invoices_url,
                        "invoices",
                        "invoice",
                        start_date,
                        end_date,
                    )

    @dlt.resource(write_disposition="replace")
    def transactions() -> Iterator[TDataItems]:
        """Fetch all transactions for the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for transactions")

        url = f"{BASE_URL}/organizations/{organization_id}/transactions"

        # Build query parameters for API-side filtering
        params = {}
        if start_date:
            from dlt.common.time import ensure_pendulum_datetime

            params["start_time"] = ensure_pendulum_datetime(start_date).format(
                "YYYY-MM-DDTHH:mm:ss"
            )

        if end_date:
            from dlt.common.time import ensure_pendulum_datetime

            params["end_time"] = ensure_pendulum_datetime(end_date).format(
                "YYYY-MM-DDTHH:mm:ss"
            )

        yield from fetch_snapchat_data_with_params(
            api, url, "transactions", "transaction", params
        )

    @dlt.resource(write_disposition="replace")
    def members() -> Iterator[TDataItems]:
        """Fetch all members of the organization."""
        if not organization_id:
            raise ValueError("organization_id is required for members")

        url = f"{BASE_URL}/organizations/{organization_id}/members"
        # Members API doesn't return updated_at in response, so we can't filter by date
        yield from fetch_snapchat_data(api, url, "members", "member", None, None)

    @dlt.resource(write_disposition="replace")
    def roles() -> Iterator[TDataItems]:
        """Fetch all roles for the organization with pagination."""
        if not organization_id:
            raise ValueError("organization_id is required for roles")

        url = f"{BASE_URL}/organizations/{organization_id}/roles"
        client = create_client()
        headers = api.get_headers()

        for result in paginate(client, headers, url, page_size=1000):
            items_data = result.get("roles", [])

            for item in items_data:
                if item.get("sub_request_status", "").upper() == "SUCCESS":
                    data = item.get("role", {})
                    if data:
                        yield data

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def campaigns(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all campaigns for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch campaigns only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get campaigns for each.
        """
        yield from fetch_with_paginate_account_id(
            api=api,
            ad_account_id=ad_account_id,
            organization_id=organization_id,
            base_url=BASE_URL,
            resource_name="campaigns",
            item_key="campaign",
            start_date=start_date,
            end_date=end_date,
        )

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def adsquads(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all ad squads for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch ad squads only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get ad squads for each.
        """
        yield from fetch_with_paginate_account_id(
            api=api,
            ad_account_id=ad_account_id,
            organization_id=organization_id,
            base_url=BASE_URL,
            resource_name="adsquads",
            item_key="adsquad",
            start_date=start_date,
            end_date=end_date,
        )

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def ads(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all ads for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch ads only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get ads for each.
        """
        yield from fetch_with_paginate_account_id(
            api=api,
            ad_account_id=ad_account_id,
            organization_id=organization_id,
            base_url=BASE_URL,
            resource_name="ads",
            item_key="ad",
            start_date=start_date,
            end_date=end_date,
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def event_details(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all event details for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch event details only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get event details for each.
        """
        yield from fetch_account_id_resource(
            api=api,
            ad_account_id=ad_account_id,
            organization_id=organization_id,
            base_url=BASE_URL,
            resource_name="event_details",
            item_key="event_detail",
            start_date=start_date,
            end_date=end_date,
        )

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def creatives(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all creatives for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch creatives only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get creatives for each.
        """
        yield from fetch_with_paginate_account_id(
            api=api,
            ad_account_id=ad_account_id,
            organization_id=organization_id,
            base_url=BASE_URL,
            resource_name="creatives",
            item_key="creative",
            start_date=start_date,
            end_date=end_date,
        )

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def segments(
        updated_at=dlt.sources.incremental("updated_at"),
    ) -> Iterator[TDataItems]:
        """Fetch all audience segments for a specific ad account or all ad accounts.

        If ad_account_id is provided, fetch segments only for that account.
        If ad_account_id is None, fetch all ad accounts first and then get segments for each.
        """
        yield from fetch_account_id_resource(
            api=api,
            ad_account_id=ad_account_id,
            organization_id=organization_id,
            base_url=BASE_URL,
            resource_name="segments",
            item_key="segment",
            start_date=start_date,
            end_date=end_date,
        )

    return (
        organizations,
        fundingsources,
        billingcenters,
        adaccounts,
        invoices,
        transactions,
        members,
        roles,
        campaigns,
        adsquads,
        ads,
        event_details,
        creatives,
        segments,
    )
