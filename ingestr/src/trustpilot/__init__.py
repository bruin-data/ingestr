"""Trustpilot source for ingesting reviews."""

from typing import Any, Dict, Generator, Iterable

import dlt
import pendulum
from dlt.sources import DltResource

from .client import TrustpilotClient


@dlt.source()
def trustpilot_source(
    business_unit_id: str,
    start_date: str,
    end_date: str | None,
    api_key: str,
    per_page: int = 1000,
) -> Iterable[DltResource]:
    """Return resources for Trustpilot."""

    client = TrustpilotClient(api_key=api_key)

    @dlt.resource(name="reviews", write_disposition="merge", primary_key="id")
    def reviews(
        dateTime=(
            dlt.sources.incremental(
                "updated_at",
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),
    ) -> Generator[Dict[str, Any], None, None]:
        if end_date is None:
            end_dt = pendulum.now(tz="UTC").isoformat()
        else:
            end_dt = dateTime.end_value
        start_dt = dateTime.last_value
        yield from client.paginated_reviews(
            business_unit_id=business_unit_id,
            per_page=per_page,
            updated_since=start_dt,
            end_date=end_dt,
        )

    yield reviews
