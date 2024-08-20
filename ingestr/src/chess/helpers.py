"""Chess source helpers"""

from dlt.common.typing import StrAny
from dlt.sources.helpers import requests

from .settings import OFFICIAL_CHESS_API_URL


def get_url_with_retry(url: str) -> StrAny:
    r = requests.get(url)
    return r.json()  # type: ignore


def get_path_with_retry(path: str) -> StrAny:
    return get_url_with_retry(f"{OFFICIAL_CHESS_API_URL}{path}")


def validate_month_string(string: str) -> None:
    """Validates that the string is in YYYY/MM format"""
    if string and string[4] != "/":
        raise ValueError(string)
