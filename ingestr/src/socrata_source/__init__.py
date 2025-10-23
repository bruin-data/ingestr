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
):
    """
    A dlt source for the Socrata open data platform.

    Fetches all records from a Socrata dataset using replace write disposition.
    Simple and straightforward - no incremental loading, no filtering.

    Args:
        domain: The Socrata domain (e.g., "evergreen.data.socrata.com")
        dataset_id: The dataset identifier (e.g., "6udu-fhnu")
        app_token: Socrata app token for higher rate limits (recommended)
        username: Username for authentication (if dataset is private)
        password: Password for authentication (if dataset is private)

    Returns:
        A dlt source with a single "dataset" resource
    """

    @dlt.resource(write_disposition="replace")
    def dataset() -> Iterator[Dict[str, Any]]:
        """
        Yields all records from a Socrata dataset.

        Uses replace write disposition - fetches full dataset each time.
        Socrata automatically provides :id, :created_at, and :updated_at fields.

        Yields:
            Dict[str, Any]: Individual records from the dataset
        """
        # Fetch all records from API
        for record in fetch_data(
            domain=domain,
            dataset_id=dataset_id,
            app_token=app_token,
            username=username,
            password=password,
        ):
            yield record

    return (dataset,)
