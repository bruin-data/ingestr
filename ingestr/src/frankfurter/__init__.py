from typing import Any, Optional, Iterator
from ingestr.src.frankfurter.helpers import FRANKFURTER_API_URL, get_path_with_retry

import dlt
from dlt.common.pendulum import pendulum
from dlt.common.typing import TAnyDateTime, datetime

@dlt.source(
    name="frankfurter",
    max_table_nesting=0,
)
def frankfurter_source(
    start_date:             Optional[TAnyDateTime] = None,
    end_date:               Optional[TAnyDateTime] = None,
    table:                  str = None,
) -> Any:
    """
    A dlt source for the frankfurter.dev API. It groups several resources (in this case frankfurter.dev API endpoints) containing
    various types of data: currencies, latest rates, historical rates.

    Returns the appropriate resource based on the provided parameters.
    """
    # Determine which resource to return based on the `table` parameter
    if table == "currencies":
        return currencies()
    
    elif table == "latest":
        return latest()
        
    elif table == "exchange_rates":
        return exchange_rates(
            start_date          =   start_date,
            end_date            =   end_date
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
        yield {
            "currency_code": currency_code,
            "currency_name": currency_name
        }



@dlt.resource(
    write_disposition="replace",
    columns={
        "date": {"data_type": "text"},
        "currency_name": {"data_type": "text"},
        "rate": {"data_type": "double"},
    },
    primary_key=["date", "currency_name"],  # Composite primary key
)
def latest() -> Iterator[dict]:
    """
    Fetches the latest exchange rates and yields them as rows.
    """
    # Base URL
    url = f"latest?"

    # Fetch data
    latest_data = get_path_with_retry(url)

    # Extract rates and base currency
    rates = latest_data["rates"]

    # Prepare the date
    date = pendulum.now().to_date_string()

    # Add the base currency (EUR) with a rate of 1.0
    yield {
        "date": date,
        "currency_name": "EUR",
        "rate": 1.0,
    }

    # Add all currencies and their rates
    for currency_name, rate in rates.items():
        yield {
            "date": date,
            "currency_name": currency_name,
            "rate": rate,
        }

   


@dlt.resource(
    write_disposition="replace",
    columns={
        "date": {"data_type": "text"},
        "currency_name": {"data_type": "text"},
        "rate": {"data_type": "double"},
    },
    primary_key=["date", "currency_name"],  # Composite primary key
)
def exchange_rates(
    start_date: Optional[TAnyDateTime] = None,
    end_date: Optional[TAnyDateTime] = None,
) -> Iterator[dict]:
    """
    Fetches exchange rates for a specified date range.
    If only start_date is provided, fetches data for that date.
    If both start_date and end_date are provided, fetches data for each day in the range.
    """

    # Normalize start_date and end_date to strings in "YYYY-MM-DD" format
    if start_date:
        start_date = start_date.strftime("%Y-%m-%d") if isinstance(start_date, datetime) else pendulum.parse(start_date).format("YYYY-MM-DD")
    else:
        start_date = pendulum.now().strftime("%Y-%m-%d")

    if end_date:
        end_date = end_date.strftime("%Y-%m-%d") if isinstance(end_date, datetime) else pendulum.parse(end_date).format("YYYY-MM-DD")
    else:
        end_date = start_date

    # Compose the URL
    url = f"{start_date}..{end_date}?"

    # Fetch data from the API
    data = get_path_with_retry(url)

    # Extract base currency and rates from the API response
    base_currency = data["base"]
    rates = data["rates"]

    # Iterate over the rates dictionary (one entry per date)
    for date, daily_rates in rates.items():
        # Add the base currency with a rate of 1.0
        yield {
            "date": date,
            "currency_name": base_currency,
            "rate": 1.0,
        }

        # Add all other currencies and their rates
        for currency_name, rate in daily_rates.items():
            yield {
                "date": date,
                "currency_name": currency_name,
                "rate": rate,
            }
