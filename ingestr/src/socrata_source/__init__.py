"""A source loading data from Socrata open data platform"""

from typing import Any, Dict, Iterator, Optional

import dlt

from .helpers import fetch_data


@dlt.source(name="socrata", max_table_nesting=0)
def source(
    domain: str,
    dataset_id: str,
    app_token: Optional[str] = None,
    username: Optional[str] = None,
    password: Optional[str] = None,
    incremental: Optional[Any] = None,
    primary_key: Optional[str] = None,
    write_disposition: Optional[str] = dlt.config.value,
):
    """
    A dlt source for the Socrata open data platform.

    Supports both full refresh (replace) and incremental loading (merge).

    Args:
        domain: The Socrata domain (e.g., "evergreen.data.socrata.com")
        dataset_id: The dataset identifier (e.g., "6udu-fhnu")
        app_token: Socrata app token for higher rate limits (recommended)
        username: Username for authentication (if dataset is private)
        password: Password for authentication (if dataset is private)
        incremental: DLT incremental object for incremental loading
        primary_key: Primary key field for merge operations (default: ":id")
        write_disposition: Write disposition ("replace", "append", "merge").
            If not provided, automatically determined based on incremental setting.

    Returns:
        A dlt source with a single "dataset" resource
    """

    @dlt.resource(
        write_disposition=write_disposition or "replace",
        primary_key=primary_key,  # type: ignore[call-overload]
    )
    def dataset(
        incremental: Optional[dlt.sources.incremental] = incremental,  # type: ignore[type-arg]
    ) -> Iterator[Dict[str, Any]]:
        """
        Yields records from a Socrata dataset.

        Supports both full refresh (replace) and incremental loading (merge).
        When incremental is provided, filters data using SoQL WHERE clause on the server side.

        Yields:
            Dict[str, Any]: Individual records from the dataset
        """
        fetch_kwargs: Dict[str, Any] = {
            "domain": domain,
            "dataset_id": dataset_id,
            "app_token": app_token,
            "username": username,
            "password": password,
        }

        if incremental and incremental.cursor_path:
            fetch_kwargs["incremental_key"] = incremental.cursor_path
            fetch_kwargs["start_value"] = (
                str(incremental.last_value)
                if incremental.last_value is not None
                else None
            )
            if getattr(incremental, "end_value", None) is not None:
                ev = incremental.end_value  # type: ignore[attr-defined]
                fetch_kwargs["end_value"] = (
                    ev.isoformat()  # type: ignore[union-attr]
                    if hasattr(ev, "isoformat")
                    else str(ev)
                )

        # Fetch and yield records
        yield from fetch_data(**fetch_kwargs)

    return (dataset,)
