"""QuickBooks source built on top of python-quickbooks."""

from typing import Iterable, Iterator, List, Optional

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from intuitlib.client import AuthClient  # type: ignore

from quickbooks import QuickBooks  # type: ignore


@dlt.source(name="quickbooks", max_table_nesting=0)
def quickbooks_source(
    company_id: str,
    start_date: pendulum.DateTime,
    object: str,
    end_date: pendulum.DateTime | None,
    client_id: str,
    client_secret: str,
    refresh_token: str,
    environment: str = "production",
    minor_version: Optional[str] = None,
) -> Iterable[DltResource]:
    """Create dlt resources for QuickBooks objects.

    Parameters
    ----------
    company_id: str
        QuickBooks company id (realm id).
    client_id: str
        OAuth client id.
    client_secret: str
        OAuth client secret.
    refresh_token: str
        OAuth refresh token.
    access_token: Optional[str]
        Optional access token. If not provided the library will refresh using the
        provided refresh token.
    environment: str
        Either ``"production"`` or ``"sandbox"``.
    minor_version: Optional[int]
        QuickBooks API minor version if needed.
    """

    auth_client = AuthClient(
        client_id=client_id,
        client_secret=client_secret,
        environment=environment,
        # redirect_uri is not used since we authenticate using refresh token which skips the step of redirect callback.
        # as redirect_uri is required param, we are passing empty string.
        redirect_uri="",
    )

    # https://help.developer.intuit.com/s/article/Validity-of-Refresh-Token
    client = QuickBooks(
        auth_client=auth_client,
        refresh_token=refresh_token,
        company_id=company_id,
        minorversion=minor_version,
    )

    def fetch_object(
        obj_name: str,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "lastupdatedtime",
            initial_value=start_date,  # type: ignore
            end_value=end_date,  # type: ignore
            range_start="closed",
            range_end="closed",
            allow_external_schedulers=True,
        ),
    ) -> Iterator[List[TDataItem]]:
        start_pos = 1

        end_dt = updated_at.end_value or pendulum.now(tz="UTC")
        start_dt = ensure_pendulum_datetime(str(updated_at.last_value)).in_tz("UTC")

        start_str = start_dt.isoformat()
        end_str = end_dt.isoformat()

        where_clause = f"WHERE MetaData.LastUpdatedTime >= '{start_str}' AND MetaData.LastUpdatedTime < '{end_str}'"
        while True:
            query = (
                f"SELECT * FROM {obj_name} {where_clause} "
                f"ORDERBY MetaData.LastUpdatedTime ASC STARTPOSITION {start_pos} MAXRESULTS 1000"
            )

            result = client.query(query)

            items = result.get("QueryResponse", {}).get(obj_name.capitalize(), [])
            if not items:
                break

            for item in items:
                if item.get("MetaData") and item["MetaData"].get("LastUpdatedTime"):
                    item["lastupdatedtime"] = ensure_pendulum_datetime(
                        item["MetaData"]["LastUpdatedTime"]
                    )
                    item["id"] = item["Id"]
                    del item["Id"]

                yield item

            if len(items) < 1000:
                break

            start_pos += 1000

    yield dlt.resource(
        fetch_object,
        name=object.lower(),
        write_disposition="merge",
        primary_key="id",
    )(object)
