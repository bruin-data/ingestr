from datetime import datetime

from dlt.common.pendulum import pendulum
from dlt.common.typing import StrAny
from dlt.sources.helpers import requests

FRANKFURTER_API_URL = "https://api.frankfurter.dev/v1/"


def get_url_with_retry(url: str) -> StrAny:
    r = requests.get(url)
    return r.json()  # type: ignore


def get_path_with_retry(path: str) -> StrAny:
    return get_url_with_retry(f"{FRANKFURTER_API_URL}{path}")


def validate_dates(start_date: datetime, end_date: datetime) -> None:
    current_date = pendulum.now()

    # Check if start_date is in the future
    if start_date > current_date:
        raise ValueError("Interval-start cannot be in the future.")

    # Check if end_date is in the future
    if end_date > current_date:
        raise ValueError("Interval-end cannot be in the future.")

    # Check if start_date is before end_date
    if start_date > end_date:
        raise ValueError("Interval-end cannot be before interval-start.")
