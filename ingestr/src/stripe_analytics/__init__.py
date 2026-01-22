# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""This source uses Stripe API and dlt to load data such as Customer, Subscription, Event etc. to the database and to calculate the MRR and churn rate."""

from typing import Any, Dict, Generator, Iterable, Optional, Tuple

import dlt
import stripe
from dlt.sources import DltResource
from pendulum import DateTime

from .helpers import (
    async_parallel_pagination,
    generate_date_ranges,
    pagination,
    transform_date,
)


@dlt.source(max_table_nesting=0)
def stripe_source(
    endpoints: Tuple[str, ...],
    stripe_secret_key: str = dlt.secrets.value,
    start_date: Optional[DateTime] = None,
    end_date: Optional[DateTime] = None,
) -> Iterable[DltResource]:
    """
    Retrieves data from the Stripe API for the specified endpoints.

    For all endpoints, Stripe API responses do not provide the key "updated",
    so in most cases, we are forced to load the data in 'replace' mode.
    This source is suitable for all types of endpoints, including 'Events', 'Invoice', etc.
    but these endpoints can also be loaded in incremental mode (see source incremental_stripe_source).

    Args:
        endpoints (Tuple[str, ...]): A tuple of endpoint names to retrieve data from. Defaults to most popular Stripe API endpoints.
        stripe_secret_key (str): The API access token for authentication. Defaults to the value in the `dlt.secrets` object.
        start_date (Optional[DateTime]): An optional start date to limit the data retrieved. Format: datetime(YYYY, MM, DD). Defaults to None.
        end_date (Optional[DateTime]): An optional end date to limit the data retrieved. Format: datetime(YYYY, MM, DD). Defaults to None.

    Returns:
        Iterable[DltResource]: Resources with data that was created during the period greater than or equal to 'start_date' and less than 'end_date'.
    """
    stripe.api_key = stripe_secret_key
    stripe.api_version = "2022-11-15"

    def stripe_resource(
        endpoint: str,
    ) -> Generator[Dict[Any, Any], Any, None]:
        yield from pagination(endpoint, start_date, end_date)

    for endpoint in endpoints:
        yield dlt.resource(
            stripe_resource,
            name=endpoint,
            write_disposition="replace",
        )(endpoint)


@dlt.source(max_table_nesting=0)
def async_stripe_source(
    endpoints: Tuple[str, ...],
    stripe_secret_key: str = dlt.secrets.value,
    start_date: Optional[DateTime] = None,
    end_date: Optional[DateTime] = None,
    max_workers: int = 4,
    rate_limit_delay: float = 0.03,
) -> Iterable[DltResource]:
    """
    ULTRA-FAST async Stripe source optimized for maximum speed and throughput.

    WARNING: Returns data in RANDOM ORDER for maximum performance.
    Uses aggressive concurrency and minimal delays to maximize API throughput.

    Args:
        endpoints (Tuple[str, ...]): A tuple of endpoint names to retrieve data from.
        stripe_secret_key (str): The API access token for authentication. Defaults to the value in the `dlt.secrets` object.
        start_date (Optional[DateTime]): An optional start date to limit the data retrieved. Format: datetime(YYYY, MM, DD). Defaults to 2010-01-01.
        end_date (Optional[DateTime]): An optional end date to limit the data retrieved. Format: datetime(YYYY, MM, DD). Defaults to today.
        max_workers (int): Maximum number of concurrent async tasks. Defaults to 40 for maximum speed.
        rate_limit_delay (float): Minimal delay between requests. Defaults to 0.03 seconds.

    Returns:
        Iterable[DltResource]: Resources with data in RANDOM ORDER (optimized for speed).
    """
    stripe.api_key = stripe_secret_key
    stripe.api_version = "2022-11-15"

    async def async_stripe_resource(endpoint: str):
        yield async_parallel_pagination(endpoint, max_workers, rate_limit_delay)

    for endpoint in endpoints:
        yield dlt.resource(
            async_stripe_resource,
            name=endpoint,
            write_disposition="replace",
        )(endpoint)


@dlt.source(max_table_nesting=0)
def incremental_stripe_source(
    endpoints: Tuple[str, ...],
    stripe_secret_key: str = dlt.secrets.value,
    initial_start_date: Optional[DateTime] = None,
    end_date: Optional[DateTime] = None,
) -> Iterable[DltResource]:
    stripe.api_key = stripe_secret_key
    stripe.api_version = "2022-11-15"
    start_date_unix = (
        transform_date(initial_start_date) if initial_start_date is not None else -1
    )

    for endpoint in endpoints:

        def date_range_resource(
            endpoint: str = endpoint,
            created: Optional[Any] = dlt.sources.incremental(
                "created",
                initial_value=start_date_unix,
                end_value=transform_date(end_date) if end_date is not None else None,
                range_end="closed",
                range_start="closed",
            ),
        ) -> Generator[Dict[str, Any], None, None]:
            from dlt.common import pendulum

            # Use 2010-01-01 as default start (Stripe founding year) to avoid
            # generating hundreds of thousands of hourly ranges from 1969
            default_start_ts = int(pendulum.datetime(2010, 1, 1).timestamp())
            start_ts = (
                created.last_value
                if created.last_value is not None
                else start_date_unix
            )
            if start_ts < 0:
                start_ts = default_start_ts
            end_ts = (
                created.end_value
                if created.end_value is not None
                else int(pendulum.now().timestamp())
            )
            for date_range in generate_date_ranges(start_ts, end_ts):
                date_range["endpoint"] = endpoint
                date_range["created"] = date_range["end_ts"]
                yield date_range

        def fetch_date_range(
            date_range: Dict[str, int],
        ) -> Generator[Dict[Any, Any], Any, None]:
            """Transformer that fetches data for a given date range."""
            yield from pagination(
                date_range["endpoint"],
                start_date=date_range["start_ts"],
                end_date=date_range["end_ts"],
            )

        date_ranges = dlt.resource(
            date_range_resource,
            name=f"{endpoint}_date_ranges",
        )()

        yield (
            date_ranges
            | dlt.transformer(
                fetch_date_range,
                name=endpoint,
                write_disposition="merge",
                primary_key="id",
                parallelized=True,
            )
        )
