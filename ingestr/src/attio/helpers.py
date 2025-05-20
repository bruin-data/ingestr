import pendulum
import requests

base_url = "https://api.attio.com/v2"


class AttioClient:
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.headers = {
            "Accept": "application/json",
            "Authorization": f"Bearer {self.api_key}",
        }

    def fetch_all_objects(
        self, url: str, client: requests.Session, limit: int = 100, params=None
    ):
        if params is None:
            params = {}
        offset = 0
        while True:
            query_params = {**params, "limit": limit, "offset": offset}
            response = client.get(url, headers=self.headers, params=query_params)

            data = response.json()["data"]
            if not data:
                break

            for item in data:
                flat_item = flat_attributes(item)
                yield flat_item

            if len(data) < limit:
                break
            offset += limit

    def fetch_all_records_of_object(
        self, url: str, client: requests.Session, limit: int = 1000, params=None
    ):
        if params is None:
            params = {}
        offset = 0
        while True:
            query_params = {**params, "limit": limit, "offset": offset}
            response = client.post(url, headers=self.headers, params=query_params)
            data = response.json()["data"]
            if not data:
                break

            for item in data:
                flat_item = flat_attributes(item)
                yield flat_item

            if len(data) < limit:
                break
            offset += limit


def flat_attributes(item: dict) -> dict:
    item["workspace_id"] = item["id"]["workspace_id"]
    item["object_id"] = item["id"]["object_id"]
    if item["id"].get("record_id") is not None:
        item["record_id"] = item["id"]["record_id"]
    item["partition_dt"] = pendulum.parse(item["created_at"]).date()  # type: ignore
    return item
