from typing import Iterable

import dlt
import pendulum
import requests
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from ingestr.src.klaviyo.client import KlaviyoClient
from ingestr.src.klaviyo.helpers import split_date_range


def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session


@dlt.source(max_table_nesting=0)
def klaviyo_source(api_key: str, start_date: TAnyDateTime) -> Iterable[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)
    client = KlaviyoClient(api_key)

    @dlt.resource(write_disposition="append", primary_key="id", parallelized=True)
    def events(
        datetime=dlt.sources.incremental(
            "datetime",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(datetime.start_value), pendulum.now()
        )

        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_events(create_client(), s, e)

    @dlt.resource(write_disposition="merge", primary_key="id", parallelized=True)
    def profiles(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(updated.start_value), pendulum.now()
        )

        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_profiles(create_client(), s, e)

    @dlt.resource(write_disposition="merge", primary_key="id", parallelized=True)
    def campaigns(
        updated_at=dlt.sources.incremental(
            "updated_at",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(updated_at.start_value), pendulum.now()
        )

        for campaign_type in ["email", "sms"]:
            for start, end in intervals:
                yield lambda s=start, e=end, ct=campaign_type: client.fetch_campaigns(
                    create_client(), s, e, ct
                )

    @dlt.resource(write_disposition="merge", primary_key="id")
    def metrics(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_metrics(create_client(), updated.start_value)

    @dlt.resource(write_disposition="replace", primary_key="id")
    def tags() -> Iterable[TAnyDateTime]:
        yield from client.fetch_tag(create_client())

    @dlt.resource(write_disposition="replace", primary_key="id")
    def coupons() -> Iterable[TAnyDateTime]:
        yield from client.fetch_coupons(create_client())

    @dlt.resource(write_disposition="merge", primary_key="id", name="catalog-variants")
    def catalog_variants(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_catalog_variant(create_client(), updated.start_value)

    @dlt.resource(
        write_disposition="merge", primary_key="id", name="catalog-categories"
    )
    def catalog_categories(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_catalog_categories(create_client(), updated.start_value)

    @dlt.resource(write_disposition="merge", primary_key="id", name="catalog-items")
    def catalog_items(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_catalog_item(create_client(), updated.start_value)

    @dlt.resource(write_disposition="append", primary_key="id", parallelized=True)
    def forms(
        updated_at=dlt.sources.incremental(
            "updated_at",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(updated_at.start_value), pendulum.now()
        )

        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_forms(create_client(), s, e)

    @dlt.resource(write_disposition="merge", primary_key="id")
    def lists(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_lists(create_client(), updated.start_value)

    @dlt.resource(write_disposition="append", primary_key="id", parallelized=True)
    def images(
        updated_at=dlt.sources.incremental(
            "updated_at",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(updated_at.start_value), pendulum.now()
        )
        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_images(create_client(), s, e)

    @dlt.resource(write_disposition="merge", primary_key="id")
    def segments(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_segments(create_client(), updated.start_value)

    @dlt.resource(write_disposition="append", primary_key="id", parallelized=True)
    def flows(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(updated.start_value), pendulum.now()
        )
        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_flows(create_client(), s, e)

    @dlt.resource(write_disposition="append", primary_key="id", parallelized=True)
    def templates(
        updated=dlt.sources.incremental(
            "updated",
            start_date_obj.isoformat(),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        intervals = split_date_range(
            pendulum.parse(updated.start_value), pendulum.now()
        )
        for start, end in intervals:
            yield lambda s=start, e=end: client.fetch_templates(create_client(), s, e)

    return (
        events,
        profiles,
        campaigns,
        metrics,
        tags,
        coupons,
        catalog_variants,
        catalog_categories,
        catalog_items,
        forms,
        lists,
        images,
        segments,
        flows,
        templates,
    )
