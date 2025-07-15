from typing import Iterable, Iterator

import dlt
import pendulum
from dlt.sources import DltResource

from .helpers import SolidgateClient

COLUMN_HINTS = {
    "subscriptions": {
        "id": {"data_type": "text", "nullable": False, "primary_key": True},
        "created_at": {"data_type": "timestamp", "partition": True},
        "status": {"data_type": "text"},
        "started_at": {"data_type": "timestamp"},
        "updated_at": {"data_type": "timestamp"},
        "expired_at": {"data_type": "timestamp"},
        "next_charge_at": {"data_type": "timestamp"},
        "payment_type": {"data_type": "text"},
        "trial": {"data_type": "bool"},
        "cancelled_at": {"data_type": "timestamp"},
        "cancellation_requested_at": {"data_type": "timestamp"},
        "cancel_code": {"data_type": "text"},
        "cancel_message": {"data_type": "text"},
        "customer": {"data_type": "json"},
        "product": {"data_type": "json"},
        "invoices": {"data_type": "json"},
    },
    "apm_orders": {
        "order_id": {"data_type": "text", "nullable": False, "primary_key": True},
        "created_at": {"data_type": "timestamp", "partition": True},
        "updated_at": {"data_type": "timestamp"},
        "order_description": {"data_type": "text"},
        "method": {"data_type": "text"},
        "amount": {"data_type": "bigint"},
        "currency": {"data_type": "text"},
        "processing_amount": {"data_type": "bigint"},
        "processing_currency": {"data_type": "text"},
        "status": {"data_type": "text"},
        "customer_account_id": {"data_type": "text"},
        "customer_email": {"data_type": "text"},
        "ip_address": {"data_type": "text"},
        "geo_country": {"data_type": "text"},
        "error_code": {"data_type": "text"},
        "transactions": {"data_type": "json"},
        "order_metadata": {"data_type": "json"},
    },
    "card_orders": {
        "order_id": {"data_type": "text", "nullable": False, "primary_key": True},
        "created_at": {"data_type": "timestamp", "partition": True},
        "updated_at": {"data_type": "timestamp"},
        "order_description": {"data_type": "text"},
        "psp_order_id": {"data_type": "text"},
        "provider_payment_id": {"data_type": "text"},
        "amount": {"data_type": "bigint"},
        "currency": {"data_type": "text"},
        "processing_amount": {"data_type": "bigint"},
        "processing_currency": {"data_type": "text"},
        "status": {"data_type": "text"},
        "payment_type": {"data_type": "text"},
        "type": {"data_type": "text"},
        "is_secured": {"data_type": "bool"},
        "routing": {"data_type": "json"},
        "customer_account_id": {"data_type": "text"},
        "customer_email": {"data_type": "text"},
        "customer_first_name": {"data_type": "text"},
        "customer_last_name": {"data_type": "text"},
        "ip_address": {"data_type": "text"},
        "mid": {"data_type": "text"},
        "traffic_source": {"data_type": "text"},
        "platform": {"data_type": "text"},
        "geo_country": {"data_type": "text"},
        "error_code": {"data_type": "text"},
        "transactions": {"data_type": "json"},
        "order_metadata": {"data_type": "json"},
        "fraudulent": {"data_type": "bool"},
    },
    "financial_entries": {
        "id": {
            "data_type": "text",
            "nullable": False,
            "primary_key": True,
        },
        "order_id": {"data_type": "text"},
        "external_psp_order_id": {"data_type": "text"},
        "created_at": {"data_type": "timestamp", "partition": True},
        "transaction_datetime_provider": {"data_type": "timestamp"},
        "transaction_datetime_utc": {"data_type": "timestamp"},
        "accounting_date": {"data_type": "date"},
        "amount": {"data_type": "double"},
        "amount_in_major_units": {"data_type": "double"},
        "currency": {"data_type": "text"},
        "currency_minor_units": {"data_type": "bigint"},
        "payout_amount": {"data_type": "double"},
        "payout_amount_in_major_units": {"data_type": "double"},
        "payout_currency": {"data_type": "text"},
        "payout_currency_minor_units": {"data_type": "bigint"},
        "record_type_key": {"data_type": "text"},
        "provider": {"data_type": "text"},
        "payment_method": {"data_type": "text"},
        "card_brand": {"data_type": "text"},
        "geo_country": {"data_type": "text"},
        "issuing_country": {"data_type": "text"},
        "transaction_id": {"data_type": "text"},
        "chargeback_id": {"data_type": "text"},
        "legal_entity": {"data_type": "text"},
    },
}


@dlt.source(max_table_nesting=0)
def solidgate_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None,
    public_key: str,
    secret_key: str,
) -> Iterable[DltResource]:
    solidgate_client = SolidgateClient(public_key, secret_key)

    @dlt.resource(
        name="subscriptions",
        write_disposition="merge",
        primary_key="id",
        columns=COLUMN_HINTS["subscriptions"],  # type: ignore
    )
    def fetch_all_subscriptions(
        dateTime=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[dict]:
        path = "subscriptions"
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value
        yield solidgate_client.fetch_data(path, date_from=start_dt, date_to=end_dt)

    @dlt.resource(
        name="apm_orders",
        write_disposition="merge",
        primary_key="order_id",
        columns=COLUMN_HINTS["apm_orders"],  # type: ignore
    )
    def fetch_apm_orders(
        dateTime=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[dict]:
        path = "apm-orders"
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value
        yield solidgate_client.fetch_data(path, date_from=start_dt, date_to=end_dt)

    @dlt.resource(
        name="card_orders",
        write_disposition="merge",
        primary_key="order_id",
        columns=COLUMN_HINTS["card_orders"],  # type: ignore
    )
    def fetch_card_orders(
        dateTime=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[dict]:
        path = "card-orders"
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value
        yield solidgate_client.fetch_data(path, date_from=start_dt, date_to=end_dt)

    @dlt.resource(
        name="financial_entries",
        write_disposition="merge",
        primary_key="id",
        columns=COLUMN_HINTS["financial_entries"],  # type: ignore
    )
    def fetch_financial_entries(
        dateTime=dlt.sources.incremental(
            "created_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ):
        if dateTime.end_value is None:
            end_date = pendulum.now(tz="UTC")
        else:
            end_date = dateTime.end_value

        start_date = dateTime.last_value
        yield solidgate_client.fetch_financial_entry_data(start_date, end_date)

    return (
        fetch_all_subscriptions,
        fetch_apm_orders,
        fetch_card_orders,
        fetch_financial_entries,
    )
