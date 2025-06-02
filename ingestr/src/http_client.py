import requests
from dlt.sources.helpers.requests import Client


def create_client(retry_status_code: int = 502) -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_status_code(retry_status_code),
        request_max_attempts=12,
        request_backoff_factor=10,
    ).session


def retry_on_status_code(retry_status_code: int):
    def retry_on_limit(
        response: requests.Response | None, exception: BaseException | None
    ) -> bool:
        if response is None:
            return False
        return response.status_code == retry_status_code

    return retry_on_limit
