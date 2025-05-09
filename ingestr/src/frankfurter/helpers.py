from datetime import datetime

from dlt.common.pendulum import pendulum
from dlt.common.typing import StrAny
from dlt.sources.helpers import requests

FRANKFURTER_API_URL = "https://api.frankfurter.dev/v1/"


def get_url_with_retry(url: str) -> StrAny:
    r = requests.get(url, timeout=5)
    return r.json()  # type: ignore


def get_path_with_retry(path: str) -> StrAny:
    return get_url_with_retry(f"{FRANKFURTER_API_URL}{path}")


def validate_dates(start_date: datetime, end_date: datetime) -> None:
    current_date = pendulum.now()

    # Check if start_date is in the futurep
    if start_date > current_date:
        raise ValueError("Interval-start cannot be in the future.")

    # Check if end_date is in the future
    if end_date > current_date:
        raise ValueError("Interval-end cannot be in the future.")

    # Check if start_date is before end_date
    if start_date > end_date:
        raise ValueError("Interval-end cannot be before interval-start.")


def validate_currency(currency_code: str) -> bool:
    url = "https://api.frankfurter.dev/v1/currencies"

    response = requests.get(url, timeout=5)
    currencies = response.json()

    if currency_code.upper() in currencies:
        return True
    else:
        supported_currencies = list(currencies.keys())
        print(
            f"Invalid base currency '{currency_code}'. Supported currencies are: {supported_currencies}"
        )
        return False
