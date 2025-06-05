import requests
from dlt.sources.helpers.requests import Client


def create_client(retry_status_codes: list[int] | None = None) -> requests.Session:
    if retry_status_codes is None:
        retry_status_codes = [502]
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_status_code(retry_status_codes),
        request_max_attempts=12,
        request_backoff_factor=10,
    ).session


def retry_on_status_code(retry_status_codes: list[int]):
    def retry_on_limit(
        response: requests.Response | None, exception: BaseException | None
    ) -> bool:
        if response is None:
            return False
        return response.status_code in retry_status_codes

    return retry_on_limit
