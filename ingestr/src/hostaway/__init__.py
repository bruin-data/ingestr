from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import HostawayClient


@dlt.source(max_table_nesting=0)
def hostaway_source(
    api_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
    listing_id: str | None = None,
) -> Iterable[DltResource]:
    """
    Hostaway API source for fetching listings and fee settings data.

    Args:
        api_key: Hostaway API key for Bearer token authentication
        start_date: Start date for incremental loading
        end_date: End date for incremental loading (defaults to current time)
        listing_id: Optional listing ID for fetching specific listing's fee settings

    Returns:
        Iterable[DltResource]: DLT resources for listings and/or fee settings
    """
    client = HostawayClient(api_key)

    @dlt.resource(
        write_disposition="merge",
        name="listings",
        primary_key="id",
    )
    def listings(
        datetime=dlt.sources.incremental(
            "latestActivityOn",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        """
        Fetch listings from Hostaway API with incremental loading.
        Uses latestActivityOn field as the incremental cursor.
        """
        if datetime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = datetime.end_value

        start_dt = datetime.last_value

        yield from client.fetch_listings(start_dt, end_dt)

    @dlt.resource(
        write_disposition="merge",
        name="listing_fee_settings",
        primary_key="id",
        table_name="listing_fee_settings",
    )
    def listing_fee_settings(
        datetime=dlt.sources.incremental(
            "updatedOn",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        """
        Fetch listing fee settings from Hostaway API with incremental loading.
        Uses updatedOn field as the incremental cursor.

        If listing_id is provided, fetches fee settings for that specific listing.
        Otherwise, fetches fee settings for all listings.
        """
        if datetime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = datetime.end_value

        start_dt = datetime.last_value

        if listing_id:
            yield from client.fetch_listing_fee_settings(listing_id, start_dt, end_dt)
        else:
            yield from client.fetch_all_listing_fee_settings(start_dt, end_dt)

    @dlt.resource(
        write_disposition="replace",
        name="listing_agreements",
        table_name="listing_agreements",
        columns={
            "id": {"data_type": "text"},
            "text": {"data_type": "text"},
            "listingMapId": {"data_type": "bigint"},
        },
    )
    def listing_agreements() -> Iterable[TDataItem]:
        """
        Fetch listing agreements from Hostaway API.

        If listing_id is provided, fetches agreements for that specific listing.
        Otherwise, fetches agreements for all listings.

        Note: Uses replace mode, so no incremental loading.
        """
        if listing_id:
            yield from client.fetch_listing_agreement(listing_id)
        else:
            very_old_date = pendulum.datetime(1970, 1, 1, tz="UTC")
            now = pendulum.now(tz="UTC")
            yield from client.fetch_all_listing_agreements(very_old_date, now)

    @dlt.resource(
        write_disposition="replace",
        name="listing_pricing_settings",
        table_name="listing_pricing_settings",
        columns={
            "id": {"data_type": "text"},
        },
    )
    def listing_pricing_settings() -> Iterable[TDataItem]:
        """
        Fetch listing pricing settings from Hostaway API.

        If listing_id is provided, fetches pricing settings for that specific listing.
        Otherwise, fetches pricing settings for all listings.

        Note: Uses replace mode, so no incremental loading.
        """
        if listing_id:
            yield from client.fetch_listing_pricing_settings(listing_id)
        else:
            very_old_date = pendulum.datetime(1970, 1, 1, tz="UTC")
            now = pendulum.now(tz="UTC")
            yield from client.fetch_all_listing_pricing_settings(very_old_date, now)

    @dlt.resource(
        write_disposition="replace",
        name="cancellation_policies",
        table_name="cancellation_policies",
    )
    def cancellation_policies() -> Iterable[TDataItem]:
        yield from client.fetch_cancellation_policies()

    @dlt.resource(
        write_disposition="replace",
        name="cancellation_policies_airbnb",
        table_name="cancellation_policies_airbnb",
    )
    def cancellation_policies_airbnb() -> Iterable[TDataItem]:
        yield from client.fetch_cancellation_policies_airbnb()

    @dlt.resource(
        write_disposition="replace",
        name="cancellation_policies_marriott",
        table_name="cancellation_policies_marriott",
    )
    def cancellation_policies_marriott() -> Iterable[TDataItem]:
        yield from client.fetch_cancellation_policies_marriott()

    @dlt.resource(
        write_disposition="replace",
        name="cancellation_policies_vrbo",
        table_name="cancellation_policies_vrbo",
    )
    def cancellation_policies_vrbo() -> Iterable[TDataItem]:
        yield from client.fetch_cancellation_policies_vrbo()

    @dlt.resource(
        write_disposition="replace",
        name="reservations",
        table_name="reservations",
        selected=False,
    )
    def reservations() -> Iterable[TDataItem]:
        yield from client.fetch_reservations()

    @dlt.transformer(
        data_from=reservations,
        write_disposition="replace",
        name="finance_fields",
        table_name="finance_fields",
    )
    def finance_fields(reservation_item: TDataItem) -> Iterable[TDataItem]:
        @dlt.defer
        def _get_finance_field(res_id):
            return list(client.fetch_finance_field(res_id))

        reservation_id = reservation_item.get("id")
        if reservation_id:
            yield _get_finance_field(reservation_id)

    @dlt.resource(
        write_disposition="replace",
        name="reservation_payment_methods",
        table_name="reservation_payment_methods",
    )
    def reservation_payment_methods() -> Iterable[TDataItem]:
        yield from client.fetch_reservation_payment_methods()

    @dlt.transformer(
        data_from=reservations,
        write_disposition="replace",
        name="reservation_rental_agreements",
        table_name="reservation_rental_agreements",
    )
    def reservation_rental_agreements(reservation_item: TDataItem) -> Iterable[TDataItem]:
        @dlt.defer
        def _get_rental_agreement(res_id):
            return list(client.fetch_reservation_rental_agreement(res_id))

        reservation_id = reservation_item.get("id")
        if reservation_id:
            yield _get_rental_agreement(reservation_id)

    @dlt.transformer(
        data_from=listings,
        write_disposition="replace",
        name="listing_calendars",
        table_name="listing_calendars",
    )
    def listing_calendars(listing_item: TDataItem) -> Iterable[TDataItem]:
        @dlt.defer
        def _get_calendar(lst_id):
            return list(client.fetch_listing_calendar(lst_id))

        listing_id_val = listing_item.get("id")
        if listing_id_val:
            yield _get_calendar(listing_id_val)

    @dlt.resource(
        write_disposition="replace",
        name="conversations",
        table_name="conversations",
    )
    def conversations() -> Iterable[TDataItem]:
        yield from client.fetch_conversations()

    @dlt.resource(
        write_disposition="replace",
        name="message_templates",
        table_name="message_templates",
    )
    def message_templates() -> Iterable[TDataItem]:
        yield from client.fetch_message_templates()

    @dlt.resource(
        write_disposition="replace",
        name="bed_types",
        table_name="bed_types",
    )
    def bed_types() -> Iterable[TDataItem]:
        yield from client.fetch_bed_types()

    @dlt.resource(
        write_disposition="replace",
        name="property_types",
        table_name="property_types",
    )
    def property_types() -> Iterable[TDataItem]:
        yield from client.fetch_property_types()

    @dlt.resource(
        write_disposition="replace",
        name="countries",
        table_name="countries",
    )
    def countries() -> Iterable[TDataItem]:
        yield from client.fetch_countries()

    @dlt.resource(
        write_disposition="replace",
        name="account_tax_settings",
        table_name="account_tax_settings",
    )
    def account_tax_settings() -> Iterable[TDataItem]:
        yield from client.fetch_account_tax_settings()

    @dlt.resource(
        write_disposition="replace",
        name="user_groups",
        table_name="user_groups",
    )
    def user_groups() -> Iterable[TDataItem]:
        yield from client.fetch_user_groups()

    @dlt.resource(
        write_disposition="replace",
        name="guest_payment_charges",
        table_name="guest_payment_charges",
    )
    def guest_payment_charges() -> Iterable[TDataItem]:
        yield from client.fetch_guest_payment_charges()

    @dlt.resource(
        write_disposition="replace",
        name="coupons",
        table_name="coupons",
    )
    def coupons() -> Iterable[TDataItem]:
        yield from client.fetch_coupons()

    @dlt.resource(
        write_disposition="replace",
        name="webhook_reservations",
        table_name="webhook_reservations",
    )
    def webhook_reservations() -> Iterable[TDataItem]:
        yield from client.fetch_webhook_reservations()

    @dlt.resource(
        write_disposition="replace",
        name="tasks",
        table_name="tasks",
    )
    def tasks() -> Iterable[TDataItem]:
        yield from client.fetch_tasks()

    return listings, listing_fee_settings, listing_agreements, listing_pricing_settings, cancellation_policies, cancellation_policies_airbnb, cancellation_policies_marriott, cancellation_policies_vrbo, reservations, finance_fields, reservation_payment_methods, reservation_rental_agreements, listing_calendars, conversations, message_templates, bed_types, property_types, countries, account_tax_settings, user_groups, guest_payment_charges, coupons, webhook_reservations, tasks
