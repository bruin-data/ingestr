from typing import Any, Optional, Iterator
from ingestr.src.frankfurter.helpers import FRANKFURTER_API_URL, get_path_with_retry

import dlt
from dlt.common.pendulum import pendulum

@dlt.source(
    name="frankfurter",
    max_table_nesting=0,
)
def frankfurter_source(
    start_date:             Optional[str] = None,
    end_date:               Optional[str] = None,
    base_currency:          Optional[str] = None,
    target_currency_list:   Optional[str] = None,
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
        return latest(base_currency=base_currency, target_currency_list=target_currency_list)
        
    elif table == "exchange_rates":
        return exchange_rates(
            start_date          =   start_date,
            end_date            =   end_date,
            base_currency       =   base_currency,
            target_currency_list=target_currency_list,
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
        "date":     {"data_type": "text"},
        "base":     {"data_type": "text"},
        "amount":   {"data_type": "double"},
        # Target currencies will be dynamically added as columns
    },
)
def latest(base_currency: Optional[str] = None, target_currency_list: Optional[str] = None) -> Iterator[dict]:
    """
    Fetches the latest exchange rates and yields them as rows.
    """
    # Base url
    url = f"latest?"

    # append parameters if given
    if base_currency:
        url += f"base={base_currency}"
    if target_currency_list:
        url += f"&symbols={target_currency_list}"

    # Fetch data
    latest_data = get_path_with_retry(url)

    # Extract rates from data
    rates = latest_data["rates"]

    # Prepare the row
    row = {
        "date":     pendulum.now().to_date_string(),
        "base":     base_currency,
        "amount":   1
    }

    # Add each currency and its exchange rate as a column
    for currency_code, exchange_rate in rates.items():
        row[currency_code] = exchange_rate

    yield row


@dlt.resource(
    write_disposition="replace",
    columns={
        "date": {"data_type": "text"},
        "base": {"data_type": "text"},
        # Target currencies will be dynamically added as columns
    },
)
def exchange_rates(
    base_currency:          Optional[str] = None,
    target_currency_list:   Optional[str] = None,
    start_date:             Optional[str] = None,
    end_date:               Optional[str] = None,
) -> Iterator[dict]:
    """
    Fetches exchange rates for a specified date range.
    If only start_date is provided, fetches data for that date.
    If both start_date and end_date are provided, fetches data for each day in the range.
    Optional base_currency and target_currency_list can be used to filter results.
    """
    
    # Append start_date and end_date to the url - NB dates validated in sources.py
    if start_date and end_date:
        url = f"{start_date}..{end_date}?"
    
    # If no start_date provided, FrankfurterSource uses today's date as default
    # If only start_date provided, url will take format: 2023-10-01..2023-10-01?
    else:
        url = f"{start_date}..{start_date}?"
    
    if base_currency:
        url += f"base={base_currency}"
    if target_currency_list:
        url += f"&symbols={target_currency_list}"

    # Fetch data from the API
    data = get_path_with_retry(url)

    # Extract base currency and rates
    rates = data["rates"]

    # Iterate over the rates dictionary (one entry per date)
    for date, daily_rates in rates.items():
        # Prepare a row with the date and base currency
        row = {
            "date":     date,
            "base":     base_currency,
            "amount":   1
        }

        # Add each target currency as a separate column
        for target_currency, rate in daily_rates.items():
            row[target_currency] = rate

        yield row
