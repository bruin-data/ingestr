"""Fetches Primer payments data."""

from typing import Iterable, Optional

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import PrimerApi


@dlt.source(name="primer", max_table_nesting=0)
def primer_source(
    api_key: str = dlt.secrets.value,
    api_version: str = "2.4",
    start_date: Optional[TAnyDateTime] = None,
    end_date: Optional[TAnyDateTime] = None,
) -> Iterable[DltResource]:
    client = PrimerApi(api_key, api_version)

    start_date_obj = ensure_pendulum_datetime(start_date) if start_date else None
    end_date_obj = ensure_pendulum_datetime(end_date) if end_date else None

    @dlt.resource(selected=False)
    def payment_ids() -> Iterable[TDataItem]:
        """List payment IDs from Primer API."""
        ids = client.list_payment_ids(
            start_date=start_date_obj,
            end_date=end_date_obj,
        )
        for payment_id in ids:
            yield {"id": payment_id}

    @dlt.transformer(
        data_from=payment_ids,
        primary_key="id",
        write_disposition="merge",
        parallelized=True,
    )
    def payments(payment_id_item: TDataItem) -> TDataItem:
        """Fetch payment details in parallel."""
        return client.get_payment(payment_id_item["id"])

    return (payment_ids, payments)
