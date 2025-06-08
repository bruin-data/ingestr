"""QuickBooks source built on top of python-quickbooks."""

from typing import Iterable, Iterator, List, Optional

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from intuitlib.client import AuthClient  # type: ignore

from quickbooks import QuickBooks  # type: ignore

# default QuickBooks objects we expose
DEFAULT_OBJECTS = [
    "Customer",
    "Invoice",
    "Account",
    "Vendor",
    "Payment",
]


@dlt.source(name="quickbooks", max_table_nesting=0)
def quickbooks_source(
    company_id: str,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
    refresh_token: str = dlt.secrets.value,
    access_token: Optional[str] = dlt.secrets.value,
    environment: str = "production",
    minor_version: Optional[int] = None,
    objects: Optional[List[str]] = None,
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
    objects: Optional[List[str]]
        List of object names to pull. Defaults to ``DEFAULT_OBJECTS``.
    """

    auth_client = AuthClient(
        client_id=client_id,
        client_secret=client_secret,
        access_token=access_token,
        environment=environment,
        redirect_uri="http://localhost",
    )

    client = QuickBooks(
        auth_client=auth_client,
        refresh_token=refresh_token,
        company_id=company_id,
        minorversion=minor_version,
    )

    objects = objects or DEFAULT_OBJECTS

    def fetch_object(
        obj_name: str,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "MetaData.LastUpdatedTime",
            initial_value=None,
            allow_external_schedulers=True,
        ),
    ) -> Iterator[List[TDataItem]]:
        start_pos = 1
        while True:
            query = (
                f"SELECT * FROM {obj_name} STARTPOSITION {start_pos} MAXRESULTS 1000"
            )
            if updated_at.last_value:
                last = ensure_pendulum_datetime(str(updated_at.last_value))
                query = (
                    f"SELECT * FROM {obj_name} WHERE MetaData.LastUpdatedTime >= '{last}' "
                    f"STARTPOSITION {start_pos} MAXRESULTS 1000"
                )
            result = client.query(query)
            items = result.get("QueryResponse", {}).get(obj_name, [])
            if not items:
                break
            yield items
            start_pos += 1000

    for obj in objects:
        yield dlt.resource(
            fetch_object,
            name=obj.lower(),
            write_disposition="merge",
            primary_key="Id",
        )(obj)
