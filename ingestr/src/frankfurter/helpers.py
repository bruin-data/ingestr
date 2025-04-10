from datetime import datetime
from typing import Optional

from dlt.common.typing import StrAny
from dlt.sources.helpers import requests

FRANKFURTER_API_URL = "https://api.frankfurter.dev/v1/"


def get_url_with_retry(url: str) -> StrAny:
    r = requests.get(url)
    return r.json()  # type: ignore


def get_path_with_retry(path: str) -> StrAny:
    return get_url_with_retry(f"{FRANKFURTER_API_URL}{path}")


def validate_dates(
    start_date: Optional[datetime], end_date: Optional[datetime]
) -> None:
    current_date = datetime.now()

    # Check if end_date is after start_date
    if start_date and end_date:
        if start_date > end_date:
            raise ValueError("End date must be after start date.")

    # Check if start_date is in the future
    if start_date and start_date > current_date:
        raise ValueError("Start date cannot be in the future.")

    # Check if end_date is in the future
    if end_date and end_date > current_date:
        raise ValueError("End date cannot be in the future.")
