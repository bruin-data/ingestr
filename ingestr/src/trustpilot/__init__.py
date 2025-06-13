"""Trustpilot source for ingesting reviews."""

from typing import Any, Dict, Generator, Iterable, Optional

import dlt
from dlt.sources import DltResource

from .client import TrustpilotClient


@dlt.source()
def trustpilot_source(
    business_unit_id: str,
    api_key: str = dlt.secrets.value,
    per_page: int = 100,
) -> Iterable[DltResource]:
    """Return resources for Trustpilot."""
    client = TrustpilotClient(api_key=api_key)

    def reviews(
        updated_at: Optional[Any] = dlt.sources.incremental(
            "updated_at", initial_value="1970-01-01T00:00:00Z"
        )
    ) -> Generator[Dict[str, Any], None, None]:
        last_value = updated_at.last_value if updated_at is not None else None
        yield from client.paginated_reviews(
            business_unit_id=business_unit_id,
            per_page=per_page,
            updated_since=last_value,
        )

    yield dlt.resource(reviews, name="reviews", write_disposition="merge", primary_key="id")
