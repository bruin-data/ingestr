from typing import Any, Iterator, Optional

import dlt
from dlt.common.pendulum import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime

from ingestr.src.frankfurter.helpers import get_path_with_retry


@dlt.source(
    name="frankfurter",
    max_table_nesting=0,
)
def frankfurter_source(
    start_date: TAnyDateTime,
    end_date: TAnyDateTime,
    base_currency: str,
) -> Any:
    """
    A dlt source for the frankfurter.dev API. It groups several resources (in this case frankfurter.dev API endpoints) containing
    various types of data: currencies, latest rates, historical rates.
    """
    date_time = dlt.sources.incremental(
        "date",
        initial_value=start_date,
        end_value=end_date,
        range_start="closed",
        range_end="closed",
    )

    return (
        currencies(),
        latest(base_currency=base_currency),
        exchange_rates(
            start_date=date_time, end_date=end_date, base_currency=base_currency
        ),
    )


@dlt.resource(
    write_disposition="replace",
    columns={
        "currency_code": {"data_type": "text"},
        "currency_name": {"data_type": "text"},
    },
)
def currencies() -> Iterator[dict]:
    """
    Yields each currency as a separate row with two columns: currency_code and currency_name.
    """
    # Retrieve the list of currencies from the API
    currencies_data = get_path_with_retry("currencies")

    for currency_code, currency_name in currencies_data.items():
        yield {"currency_code": currency_code, "currency_name": currency_name}


@dlt.resource(
    write_disposition="merge",
    columns={
        "date": {"data_type": "text"},
        "currency_code": {"data_type": "text"},
        "rate": {"data_type": "double"},
        "base_currency": {"data_type": "text"},
    },
    primary_key=["date", "currency_code", "base_currency"],
)
def latest(base_currency: Optional[str] = "") -> Iterator[dict]:
    """
    Fetches the latest exchange rates and yields them as rows.
    """
    # Base URL
    url = "latest?"

    if base_currency:
        url += f"base={base_currency}"

    # Fetch data
    data = get_path_with_retry(url)

    # Extract rates and base currency
    rates = data["rates"]
    date = pendulum.parse(data["date"])

    # Add the base currency with a rate of 1.0
    yield {
        "date": date,
        "currency_code": base_currency,
        "rate": 1.0,
        "base_currency": base_currency,
    }

    # Add all currencies and their rates
    for currency_code, rate in rates.items():
        yield {
            "date": date,
            "currency_code": currency_code,
            "rate": rate,
            "base_currency": base_currency,
        }


@dlt.resource(
    write_disposition="merge",
    columns={
        "date": {"data_type": "text"},
        "currency_code": {"data_type": "text"},
        "rate": {"data_type": "double"},
        "base_currency": {"data_type": "text"},
    },
    primary_key=("date", "currency_code", "base_currency"),
)
def exchange_rates(
    end_date: TAnyDateTime,
    start_date: dlt.sources.incremental[TAnyDateTime] = dlt.sources.incremental("date"),
    base_currency: Optional[str] = "",
) -> Iterator[dict]:
    """
    Fetches exchange rates for a specified date range.
    If only start_date is provided, fetches data until now.
    If both start_date and end_date are provided, fetches data for each day in the range.
    """
    # Ensure start_date.last_value is a pendulum.DateTime object
    start_date_obj = ensure_pendulum_datetime(start_date.last_value)  # type: ignore
    start_date_str = start_date_obj.format("YYYY-MM-DD")

    # Ensure end_date is a pendulum.DateTime object
    end_date_obj = ensure_pendulum_datetime(end_date)
    end_date_str = end_date_obj.format("YYYY-MM-DD")

    # Compose the URL
    url = f"{start_date_str}..{end_date_str}?"

    if base_currency:
        url += f"base={base_currency}"

    # Fetch data from the API
    data = get_path_with_retry(url)

    # Extract base currency and rates from the API response
    rates = data["rates"]

    # Iterate over the rates dictionary (one entry per date)
    for date, daily_rates in rates.items():
        formatted_date = pendulum.parse(date)

        # Add the base currency with a rate of 1.0
        yield {
            "date": formatted_date,
            "currency_code": base_currency,
            "rate": 1.0,
            "base_currency": base_currency,
        }

        # Add all other currencies and their rates
        for currency_code, rate in daily_rates.items():
            yield {
                "date": formatted_date,
                "currency_code": currency_code,
                "rate": rate,
                "base_currency": base_currency,
            }
