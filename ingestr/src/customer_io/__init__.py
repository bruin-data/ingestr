from datetime import datetime
from typing import Iterable

import dlt
import pendulum
import requests
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from ingestr.src.customer_io.helpers import CustomerIoClient


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
def customer_io_source(
    api_key: str,
    region: str = "us",
    start_date: TAnyDateTime | None = None,
    end_date: TAnyDateTime | None = None,
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key, region)

    @dlt.resource(write_disposition="replace", primary_key="id")
    def activities() -> Iterable[TDataItem]:
        yield from client.fetch_activities(create_client())

    @dlt.resource(write_disposition="merge", primary_key="id")
    def broadcasts(
        updated=dlt.sources.incremental(
            "updated",
            initial_value=start_date or pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        last_value = pendulum.instance(updated.last_value).in_tz("UTC") if updated.last_value else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_value = pendulum.instance(updated.end_value).in_tz("UTC") if updated.end_value else None
        for item in client.fetch_broadcasts(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            if item_updated >= last_value:
                if end_value is None or item_updated <= end_value:
                    item["updated"] = item_updated
                    yield item

    @dlt.transformer(data_from=broadcasts, write_disposition="merge", primary_key="id")
    def broadcast_actions(broadcast: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = broadcast.get("id")
        start_val = pendulum.instance(start_date).in_tz("UTC") if start_date else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_val = pendulum.instance(end_date).in_tz("UTC") if end_date else None
        for item in client.fetch_broadcast_actions(create_client(), broadcast_id):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            if item_updated >= start_val:
                if end_val is None or item_updated <= end_val:
                    item["updated"] = item_updated
                    yield item

    @dlt.transformer(data_from=broadcasts, write_disposition="merge", primary_key="id")
    def broadcast_messages(broadcast: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = broadcast.get("id")
        start_ts = int(start_date.timestamp()) if start_date else None
        end_ts = int(end_date.timestamp()) if end_date else None
        for item in client.fetch_broadcast_messages(
            create_client(), broadcast_id, start_ts, end_ts
        ):
            yield item

    @dlt.resource(write_disposition="merge", primary_key="id")
    def campaigns(
        updated=dlt.sources.incremental(
            "updated",
            initial_value=start_date or pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        last_value = pendulum.instance(updated.last_value).in_tz("UTC") if updated.last_value else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_value = pendulum.instance(updated.end_value).in_tz("UTC") if updated.end_value else None
        for item in client.fetch_campaigns(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            if item_updated >= last_value:
                if end_value is None or item_updated <= end_value:
                    item["updated"] = item_updated
                    yield item

    @dlt.transformer(data_from=campaigns, write_disposition="merge", primary_key="id")
    def campaign_actions(campaign: TDataItem) -> Iterable[TDataItem]:
        campaign_id = campaign.get("id")
        for item in client.fetch_campaign_actions(create_client(), campaign_id):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            item["updated"] = item_updated
            yield item

    @dlt.resource(write_disposition="merge", primary_key="id")
    def collections(
        updated_at=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date or pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        last_value = pendulum.instance(updated_at.last_value).in_tz("UTC") if updated_at.last_value else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_value = pendulum.instance(updated_at.end_value).in_tz("UTC") if updated_at.end_value else None
        for item in client.fetch_collections(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated_at", 0))
            if item_updated >= last_value:
                if end_value is None or item_updated <= end_value:
                    item["updated_at"] = item_updated
                    yield item

    @dlt.resource(write_disposition="merge", primary_key="id")
    def exports(
        updated_at=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date or pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        last_value = pendulum.instance(updated_at.last_value).in_tz("UTC") if updated_at.last_value else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_value = pendulum.instance(updated_at.end_value).in_tz("UTC") if updated_at.end_value else None
        for item in client.fetch_exports(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated_at", 0))
            if item_updated >= last_value:
                if end_value is None or item_updated <= end_value:
                    item["updated_at"] = item_updated
                    yield item

    @dlt.resource(write_disposition="replace", primary_key="ip")
    def info_ip_addresses() -> Iterable[TDataItem]:
        yield from client.fetch_info_ip_addresses(create_client())

    @dlt.resource(write_disposition="merge", primary_key="id")
    def messages() -> Iterable[TDataItem]:
        start_ts = int(start_date.timestamp()) if start_date else None
        end_ts = int(end_date.timestamp()) if end_date else None
        yield from client.fetch_messages(create_client(), start_ts, end_ts)

    @dlt.resource(write_disposition="merge", primary_key="id")
    def newsletters(
        updated=dlt.sources.incremental(
            "updated",
            initial_value=start_date or pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        last_value = pendulum.instance(updated.last_value).in_tz("UTC") if updated.last_value else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_value = pendulum.instance(updated.end_value).in_tz("UTC") if updated.end_value else None
        for item in client.fetch_newsletters(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            if item_updated >= last_value:
                if end_value is None or item_updated <= end_value:
                    item["updated"] = item_updated
                    yield item

    @dlt.transformer(data_from=newsletters, write_disposition="replace", primary_key="id")
    def newsletter_test_groups(newsletter: TDataItem) -> Iterable[TDataItem]:
        newsletter_id = newsletter.get("id")
        yield from client.fetch_newsletter_test_groups(create_client(), newsletter_id)

    @dlt.resource(write_disposition="replace", primary_key="id")
    def reporting_webhooks() -> Iterable[TDataItem]:
        yield from client.fetch_reporting_webhooks(create_client())

    @dlt.resource(write_disposition="merge", primary_key="id")
    def segments(
        updated_at=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date or pendulum.datetime(1970, 1, 1, tz="UTC"),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        last_value = pendulum.instance(updated_at.last_value).in_tz("UTC") if updated_at.last_value else pendulum.datetime(1970, 1, 1, tz="UTC")
        end_value = pendulum.instance(updated_at.end_value).in_tz("UTC") if updated_at.end_value else None
        for item in client.fetch_segments(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated_at", 0))
            if item_updated >= last_value:
                if end_value is None or item_updated <= end_value:
                    item["updated_at"] = item_updated
                    yield item

    @dlt.resource(write_disposition="replace", primary_key="id")
    def transactional_messages() -> Iterable[TDataItem]:
        yield from client.fetch_transactional_messages(create_client())

    @dlt.resource(write_disposition="replace", primary_key="id")
    def workspaces() -> Iterable[TDataItem]:
        yield from client.fetch_workspaces(create_client())

    @dlt.resource(write_disposition="replace", primary_key="id")
    def sender_identities() -> Iterable[TDataItem]:
        yield from client.fetch_sender_identities(create_client())

    @dlt.resource(write_disposition="replace", primary_key="cio_id")
    def customers() -> Iterable[TDataItem]:
        yield from client.fetch_customers(create_client())

    @dlt.transformer(data_from=customers, write_disposition="replace", primary_key="customer_id")
    def customer_attributes(customer: TDataItem) -> Iterable[TDataItem]:
        customer_id = customer.get("cio_id") or customer.get("id")
        if customer_id:
            result = client.fetch_customer_attributes(create_client(), customer_id)
            if result:
                yield result

    @dlt.transformer(data_from=customers, write_disposition="merge", primary_key="id")
    def customer_messages(customer: TDataItem) -> Iterable[TDataItem]:
        customer_id = customer.get("cio_id") or customer.get("id")
        if customer_id:
            start_ts = int(start_date.timestamp()) if start_date else None
            end_ts = int(end_date.timestamp()) if end_date else None
            yield from client.fetch_customer_messages(create_client(), customer_id, start_ts, end_ts)

    @dlt.transformer(data_from=customers, write_disposition="replace", primary_key="id")
    def customer_activities(customer: TDataItem) -> Iterable[TDataItem]:
        customer_id = customer.get("cio_id") or customer.get("id")
        if customer_id:
            yield from client.fetch_customer_activities(create_client(), customer_id)

    @dlt.transformer(data_from=customers, write_disposition="replace", primary_key=["customer_id", "object_type_id", "object_id"])
    def customer_relationships(customer: TDataItem) -> Iterable[TDataItem]:
        customer_id = customer.get("cio_id") or customer.get("id")
        if customer_id:
            for rel in client.fetch_customer_relationships(create_client(), customer_id):
                identifiers = rel.get("identifiers", {})
                rel["object_id"] = identifiers.get("object_id") or identifiers.get("cio_object_id")
                yield rel

    @dlt.resource(write_disposition="replace", primary_key="id")
    def object_types() -> Iterable[TDataItem]:
        yield from client.fetch_object_types(create_client())

    @dlt.transformer(data_from=object_types, write_disposition="replace", primary_key=["object_type_id", "object_id"])
    def objects(obj_type: TDataItem) -> Iterable[TDataItem]:
        object_type_id = obj_type.get("id")
        if object_type_id:
            yield from client.fetch_objects(create_client(), str(object_type_id))

    @dlt.resource(write_disposition="replace", primary_key="id")
    def subscription_topics() -> Iterable[TDataItem]:
        yield from client.fetch_subscription_topics(create_client())

    @dlt.transformer(data_from=campaigns, write_disposition="merge", primary_key="id")
    def campaign_messages(campaign: TDataItem) -> Iterable[TDataItem]:
        campaign_id = campaign.get("id")
        start_ts = int(start_date.timestamp()) if start_date else None
        end_ts = int(end_date.timestamp()) if end_date else None
        yield from client.fetch_campaign_messages(create_client(), campaign_id, start_ts, end_ts)

    return (activities, broadcasts, broadcast_actions, broadcast_messages, campaigns, campaign_actions, campaign_messages, collections, exports, info_ip_addresses, messages, newsletters, newsletter_test_groups, reporting_webhooks, segments, sender_identities, transactional_messages, workspaces, customers, customer_attributes, customer_messages, customer_activities, customer_relationships, object_types, objects, subscription_topics)


@dlt.source(max_table_nesting=0)
def customer_io_broadcast_metrics_source(
    api_key: str,
    region: str = "us",
    period: str = "days",
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key, region)

    @dlt.resource(
        write_disposition="replace"
    )
    def broadcast_metrics() -> Iterable[TDataItem]:
        yield from client.fetch_broadcast_metrics(create_client(), period)

    return (broadcast_metrics,)


@dlt.source(max_table_nesting=0)
def customer_io_broadcast_action_metrics_source(
    api_key: str,
    region: str = "us",
    period: str = "days",
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key, region)

    @dlt.resource(write_disposition="replace", primary_key="id", selected=False)
    def broadcasts() -> Iterable[TDataItem]:
        for item in client.fetch_broadcasts(create_client()):
            yield item

    @dlt.transformer(data_from=broadcasts, write_disposition="replace", selected=False)
    def broadcast_actions(broadcast: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = broadcast.get("id")
        for item in client.fetch_broadcast_actions(create_client(), broadcast_id):
            yield item

    @dlt.transformer(
        data_from=broadcast_actions,
        write_disposition="replace",
        primary_key=["broadcast_id", "action_id", "period", "step_index"],
    )
    def broadcast_action_metrics(action: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = action.get("broadcast_id")
        action_id = action.get("id")
        for item in client.fetch_broadcast_action_metrics(
            create_client(), broadcast_id, action_id, period
        ):
            yield item

    return (broadcasts | broadcast_actions | broadcast_action_metrics,)


@dlt.source(max_table_nesting=0)
def customer_io_campaign_metrics_source(
    api_key: str,
    region: str = "us",
    period: str = "days",
    start_date: TAnyDateTime | None = None,
    end_date: TAnyDateTime | None = None,
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key, region)

    start_ts = int(start_date.timestamp()) if start_date else None
    end_ts = int(end_date.timestamp()) if end_date else None

    @dlt.resource(write_disposition="replace", primary_key="id", selected=False)
    def campaigns() -> Iterable[TDataItem]:
        for item in client.fetch_campaigns(create_client()):
            yield item

    @dlt.transformer(
        data_from=campaigns,
        write_disposition="replace",
        primary_key=["campaign_id", "period", "step_index"],
    )
    def campaign_metrics(campaign: TDataItem) -> Iterable[TDataItem]:
        campaign_id = campaign.get("id")
        for item in client.fetch_campaign_metrics(
            create_client(), campaign_id, period, start_ts, end_ts
        ):
            yield item

    return (campaigns | campaign_metrics,)


@dlt.source(max_table_nesting=0)
def customer_io_campaign_action_metrics_source(
    api_key: str,
    region: str = "us",
    period: str = "days",
    start_date: TAnyDateTime | None = None,
    end_date: TAnyDateTime | None = None,
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key, region)

    start_ts = int(start_date.timestamp()) if start_date else None
    end_ts = int(end_date.timestamp()) if end_date else None

    @dlt.resource(write_disposition="replace", primary_key="id", selected=False)
    def campaigns() -> Iterable[TDataItem]:
        for item in client.fetch_campaigns(create_client()):
            yield item

    @dlt.transformer(data_from=campaigns, write_disposition="replace", selected=False)
    def campaign_actions(campaign: TDataItem) -> Iterable[TDataItem]:
        campaign_id = campaign.get("id")
        for item in client.fetch_campaign_actions(create_client(), campaign_id):
            yield item

    @dlt.transformer(
        data_from=campaign_actions,
        write_disposition="replace",
        primary_key=["campaign_id", "action_id", "period", "step_index"],
    )
    def campaign_action_metrics(action: TDataItem) -> Iterable[TDataItem]:
        campaign_id = action.get("campaign_id")
        action_id = action.get("id")
        for item in client.fetch_campaign_action_metrics(
            create_client(), campaign_id, action_id, period, start_ts, end_ts
        ):
            yield item

    return (campaigns | campaign_actions | campaign_action_metrics,)


@dlt.source(max_table_nesting=0)
def customer_io_newsletter_metrics_source(
    api_key: str,
    region: str = "us",
    period: str = "days",
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key, region)

    @dlt.resource(write_disposition="replace", primary_key="id", selected=False)
    def newsletters() -> Iterable[TDataItem]:
        for item in client.fetch_newsletters(create_client()):
            yield item

    @dlt.transformer(
        data_from=newsletters,
        write_disposition="replace",
        primary_key=["newsletter_id", "period", "step_index"],
    )
    def newsletter_metrics(newsletter: TDataItem) -> Iterable[TDataItem]:
        newsletter_id = newsletter.get("id")
        for item in client.fetch_newsletter_metrics(
            create_client(), newsletter_id, period
        ):
            yield item

    return (newsletters | newsletter_metrics,)
