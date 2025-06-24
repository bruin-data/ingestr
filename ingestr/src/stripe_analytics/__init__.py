"""This source uses Stripe API and dlt to load data such as Customer, Subscription, Event etc. to the database and to calculate the MRR and churn rate."""

from typing import Any, Dict, Generator, Iterable, Optional, Tuple

import dlt
import stripe
from dlt.sources import DltResource
from pendulum import DateTime

from .helpers import (
    async_parallel_pagination,
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


@dlt.source
def incremental_stripe_source(
    endpoints: Tuple[str, ...],
    stripe_secret_key: str = dlt.secrets.value,
    initial_start_date: Optional[DateTime] = None,
    end_date: Optional[DateTime] = None,
) -> Iterable[DltResource]:
    """
    As Stripe API does not include the "updated" key in its responses,
    we are only able to perform incremental downloads from endpoints where all objects are uneditable.
    This source yields the resources with incremental loading based on "append" mode.
    You will load only the newest data without duplicating and without downloading a huge amount of data each time.

    Args:
        endpoints (tuple): A tuple of endpoint names to retrieve data from. Defaults to Stripe API endpoints with uneditable data.
        stripe_secret_key (str): The API access token for authentication. Defaults to the value in the `dlt.secrets` object.
        initial_start_date (Optional[DateTime]): An optional parameter that specifies the initial value for dlt.sources.incremental.
                            If parameter is not None, then load only data that were created after initial_start_date on the first run.
                            Defaults to None. Format: datetime(YYYY, MM, DD).
        end_date (Optional[DateTime]): An optional end date to limit the data retrieved.
                  Defaults to None. Format: datetime(YYYY, MM, DD).
    Returns:
        Iterable[DltResource]: Resources with only that data has not yet been loaded.
    """
    stripe.api_key = stripe_secret_key
    stripe.api_version = "2022-11-15"
    start_date_unix = (
        transform_date(initial_start_date) if initial_start_date is not None else -1
    )

    def incremental_resource(
        endpoint: str,
        created: Optional[Any] = dlt.sources.incremental(
            "created",
            initial_value=start_date_unix,
            end_value=transform_date(end_date) if end_date is not None else None,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Generator[Dict[Any, Any], Any, None]:
        yield from pagination(
            endpoint, start_date=created.last_value, end_date=created.end_value
        )

    for endpoint in endpoints:
        yield dlt.resource(
            incremental_resource,
            name=endpoint,
            write_disposition="append",
            primary_key="id",
        )(endpoint)
