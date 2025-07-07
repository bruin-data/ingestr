"""Stripe analytics source helpers"""

import asyncio
import math
from datetime import datetime, timedelta
from typing import Any, Dict, Iterable, List, Optional, Union

import stripe
from dlt.common import pendulum
from dlt.common.typing import TDataItem
from pendulum import DateTime


def pagination(
    endpoint: str, start_date: Optional[Any] = None, end_date: Optional[Any] = None
) -> Iterable[TDataItem]:
    """
    Retrieves data from an endpoint with pagination.

    Args:
        endpoint (str): The endpoint to retrieve data from.
        start_date (Optional[Any]): An optional start date to limit the data retrieved. Defaults to None.
        end_date (Optional[Any]): An optional end date to limit the data retrieved. Defaults to None.

    Returns:
        Iterable[TDataItem]: Data items retrieved from the endpoint.
    """
    starting_after = None
    while True:
        response = stripe_get_data(
            endpoint,
            start_date=start_date,
            end_date=end_date,
            starting_after=starting_after,
        )

        if len(response["data"]) > 0:
            starting_after = response["data"][-1]["id"]
        yield response["data"]

        if not response["has_more"]:
            break


def _create_time_chunks(start_ts: int, end_ts: int, num_chunks: int) -> List[tuple]:
    """
    Divide a time range into equal chunks for parallel processing.

    Args:
        start_ts (int): Start timestamp
        end_ts (int): End timestamp
        num_chunks (int): Number of chunks to create

    Returns:
        List[tuple]: List of (chunk_start, chunk_end) timestamp pairs
    """
    total_duration = end_ts - start_ts
    chunk_duration = math.ceil(total_duration / num_chunks)

    chunks = []
    current_start = start_ts

    for i in range(num_chunks):
        current_end = min(current_start + chunk_duration, end_ts)
        if current_start < end_ts:
            chunks.append((current_start, current_end))
        current_start = current_end

        if current_start >= end_ts:
            break

    return chunks


def _create_adaptive_time_chunks(
    start_ts: int, end_ts: int, max_workers: int
) -> List[tuple]:
    """
    Create time chunks with adaptive sizing - larger chunks for 2010s (less data expected).

    Args:
        start_ts (int): Start timestamp
        end_ts (int): End timestamp
        max_workers (int): Maximum number of workers

    Returns:
        List[tuple]: List of (chunk_start, chunk_end) timestamp pairs
    """
    chunks = []

    # Key timestamps
    year_2020_ts = int(pendulum.datetime(2020, 1, 1).timestamp())
    year_2015_ts = int(pendulum.datetime(2015, 1, 1).timestamp())

    current_start = start_ts

    # Handle 2010-2015: Large chunks (2-3 year periods)
    if current_start < year_2015_ts:
        chunk_end = min(year_2015_ts, end_ts)
        if current_start < chunk_end:
            # Split 2010-2015 into 2-3 chunks max
            pre_2015_chunks = _create_time_chunks(
                current_start, chunk_end, min(3, max_workers)
            )
            chunks.extend(pre_2015_chunks)
        current_start = chunk_end

    # Handle 2015-2020: Medium chunks (6 month to 1 year periods)
    if current_start < year_2020_ts and current_start < end_ts:
        chunk_end = min(year_2020_ts, end_ts)
        if current_start < chunk_end:
            # Split 2015-2020 into smaller chunks
            duration_2015_2020 = chunk_end - current_start
            years_2015_2020 = duration_2015_2020 / (365 * 24 * 60 * 60)
            num_chunks_2015_2020 = min(
                max_workers, max(2, int(years_2015_2020 * 2))
            )  # ~6 months per chunk

            pre_2020_chunks = _create_time_chunks(
                current_start, chunk_end, num_chunks_2015_2020
            )
            chunks.extend(pre_2020_chunks)
        current_start = chunk_end

    if current_start < end_ts:
        # Split post-2020 data into daily chunks for maximum granularity
        current_chunk_start = current_start
        while current_chunk_start < end_ts:
            # Calculate end of current day
            current_date = datetime.fromtimestamp(current_chunk_start)
            next_day = current_date + timedelta(days=1)
            chunk_end = min(int(next_day.timestamp()), end_ts)

            chunks.append((current_chunk_start, chunk_end))
            current_chunk_start = chunk_end

    return chunks


def _fetch_chunk_data_streaming(
    endpoint: str, start_ts: int, end_ts: int
) -> List[List[TDataItem]]:
    """
    Fetch data for a specific time chunk using sequential pagination with memory-efficient approach.

    Args:
        endpoint (str): The Stripe endpoint to fetch from
        start_ts (int): Start timestamp for this chunk
        end_ts (int): End timestamp for this chunk

    Returns:
        List[List[TDataItem]]: List of batches of data items
    """
    # For streaming, we still need to collect the chunk data to maintain order
    # but we can optimize by not holding all data in memory at once
    print(
        f"Fetching chunk {datetime.fromtimestamp(start_ts).strftime('%Y-%m-%d')}-{datetime.fromtimestamp(end_ts).strftime('%Y-%m-%d')}"
    )
    chunk_data = []
    batch_count = 0

    for batch in pagination(endpoint, start_ts, end_ts):
        chunk_data.append(batch)
        print(
            f"Processed {batch_count} batches for chunk {datetime.fromtimestamp(start_ts).strftime('%Y-%m-%d')}-{datetime.fromtimestamp(end_ts).strftime('%Y-%m-%d')}"
        )
        batch_count += 1

    return chunk_data


async def async_pagination(
    endpoint: str, start_date: Optional[Any] = None, end_date: Optional[Any] = None
) -> Iterable[TDataItem]:
    """
    Async version of pagination that retrieves data from an endpoint with pagination.

    Args:
        endpoint (str): The endpoint to retrieve data from.
        start_date (Optional[Any]): An optional start date to limit the data retrieved. Defaults to None.
        end_date (Optional[Any]): An optional end date to limit the data retrieved. Defaults to None.

    Returns:
        Iterable[TDataItem]: Data items retrieved from the endpoint.
    """
    starting_after = None
    while True:
        response = await stripe_get_data_async(
            endpoint,
            start_date=start_date,
            end_date=end_date,
            starting_after=starting_after,
        )

        if len(response["data"]) > 0:
            starting_after = response["data"][-1]["id"]
        yield response["data"]

        if not response["has_more"]:
            break


async def async_parallel_pagination(
    endpoint: str,
    max_workers: int = 8,
    rate_limit_delay: float = 5,
) -> Iterable[TDataItem]:
    """
    ULTRA-FAST async parallel pagination - yields data in random order for maximum speed.
    No ordering constraints - pure performance optimization.

    Args:
        endpoint (str): The endpoint to retrieve data from.
        start_date (Optional[Any]): An optional start date to limit the data retrieved. Defaults to 2010-01-01 if None.
        end_date (Optional[Any]): An optional end date to limit the data retrieved. Defaults to today if None.
        max_workers (int): Maximum number of concurrent async tasks. Defaults to 8 for balanced speed/rate limit respect.
        rate_limit_delay (float): Minimal delay between requests. Defaults to 5 seconds.

    Returns:
        Iterable[TDataItem]: Data items retrieved from the endpoint (RANDOM ORDER FOR SPEED).
    """

    start_date = pendulum.datetime(2010, 1, 1)
    end_date = pendulum.now()
    start_ts = transform_date(start_date)
    end_ts = transform_date(end_date)

    # Create time chunks with larger chunks for 2010s (less data expected)
    time_chunks = _create_adaptive_time_chunks(start_ts, end_ts, max_workers)

    # Use asyncio semaphore to control concurrency and respect rate limits
    semaphore = asyncio.Semaphore(max_workers)

    async def fetch_chunk_with_semaphore(chunk_start: int, chunk_end: int):
        async with semaphore:
            return await _fetch_chunk_data_async_fast(endpoint, chunk_start, chunk_end)

    # Create all tasks
    tasks = [
        fetch_chunk_with_semaphore(chunk_start, chunk_end)
        for chunk_start, chunk_end in time_chunks
    ]

    for coro in asyncio.as_completed(tasks):
        try:
            chunk_data = await coro

            for batch in chunk_data:
                yield batch

        except Exception as exc:
            print(f"Async chunk processing generated an exception: {exc}")
            raise exc


async def _fetch_chunk_data_async_fast(
    endpoint: str, start_ts: int, end_ts: int
) -> List[List[TDataItem]]:
    """
    ULTRA-FAST async chunk fetcher - no metadata overhead, direct data return.

    Args:
        endpoint (str): The Stripe endpoint to fetch from
        start_ts (int): Start timestamp for this chunk
        end_ts (int): End timestamp for this chunk

    Returns:
        List[List[TDataItem]]: Raw batches with zero overhead
    """
    chunk_data = []
    async for batch in async_pagination(endpoint, start_ts, end_ts):
        chunk_data.append(batch)

    return chunk_data


def transform_date(date: Union[str, DateTime, int]) -> int:
    if isinstance(date, str):
        date = pendulum.from_format(date, "%Y-%m-%dT%H:%M:%SZ")
    if isinstance(date, DateTime):
        # convert to unix timestamp
        date = int(date.timestamp())
    return date


def stripe_get_data(
    resource: str,
    start_date: Optional[Any] = None,
    end_date: Optional[Any] = None,
    **kwargs: Any,
) -> Dict[Any, Any]:
    if start_date:
        start_date = transform_date(start_date)
    if end_date:
        end_date = transform_date(end_date)

    if resource == "Subscription":
        kwargs.update({"status": "all"})

    resource_dict = getattr(stripe, resource).list(
        created={"gte": start_date, "lt": end_date}, limit=100, **kwargs
    )
    return dict(resource_dict)


async def stripe_get_data_async(
    resource: str,
    start_date: Optional[Any] = None,
    end_date: Optional[Any] = None,
    **kwargs: Any,
) -> Dict[Any, Any]:
    """Async version of stripe_get_data"""
    if start_date:
        start_date = transform_date(start_date)
    if end_date:
        end_date = transform_date(end_date)

    if resource == "Subscription":
        kwargs.update({"status": "all"})

    import asyncio

    from stripe import RateLimitError

    max_retries = 50
    retry_count = 0
    max_wait_time_ms = 10000

    while retry_count < max_retries:
        # print(
        #     f"Fetching {resource} from {datetime.fromtimestamp(start_date).strftime('%Y-%m-%d %H:%M:%S') if start_date else 'None'} to {datetime.fromtimestamp(end_date).strftime('%Y-%m-%d %H:%M:%S') if end_date else 'None'}, retry {retry_count} of {max_retries}",
        #     flush=True,
        # )
        try:
            resource_dict = await getattr(stripe, resource).list_async(
                created={"gte": start_date, "lt": end_date}, limit=100, **kwargs
            )
            return dict(resource_dict)
        except RateLimitError:
            retry_count += 1
            if retry_count < max_retries:
                wait_time = min(2**retry_count * 0.001, max_wait_time_ms)
                print(
                    f"Got rate limited, sleeping {wait_time} seconds before retrying...",
                    flush=True,
                )
                await asyncio.sleep(wait_time)
            else:
                # Re-raise the last exception if we've exhausted retries
                print(f"✗ Failed to fetch {resource} after {max_retries} retries")
                raise

    return dict(resource_dict)
