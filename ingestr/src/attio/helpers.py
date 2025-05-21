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

    def fetch_attributes(
        self, url: str, client: requests.Session, limit: int = 1000, params=None
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

    def fetch_all_records(
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

    def fetch_all_list_entries_for_object(
        self, client: requests.Session, object_id: str | None
    ):
        url = f"{base_url}/lists"
        for lst in self.fetch_attributes(url, client):
            if object_id in lst["parent_object"]:
                url = f"{base_url}/lists/{lst['id']['list_id']}/entries/query"
                for entry in self.fetch_all_records(url, client):
                    yield entry


def flat_attributes(item: dict) -> dict:
    item["workspace_id"] = item["id"]["workspace_id"]
    if item["id"].get("object_id") is not None:
        item["object_id"] = item["id"]["object_id"]
    if item["id"].get("record_id") is not None:
        item["record_id"] = item["id"]["record_id"]
    if item["id"].get("list_id") is not None:
        item["list_id"] = item["id"]["list_id"]
    if item["id"].get("entry_id") is not None:
        item["entry_id"] = item["id"]["entry_id"]
    item["partition_dt"] = pendulum.parse(item["created_at"]).date()  # type: ignore
    return item
