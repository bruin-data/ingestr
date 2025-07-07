from ingestr.src.http_client import create_client


class AttioClient:
    def __init__(self, api_key: str):
        self.base_url = "https://api.attio.com/v2"
        self.headers = {
            "Accept": "application/json",
            "Authorization": f"Bearer {api_key}",
        }
        self.client = create_client()

    def fetch_paginated(self, path: str, method: str, limit: int = 1000, params=None):
        url = f"{self.base_url}/{path}"
        if params is None:
            params = {}
        offset = 0
        while True:
            query_params = {"limit": limit, "offset": offset, **params}
            if method == "get":
                response = self.client.get(
                    url, headers=self.headers, params=query_params
                )
            else:
                json_body = {**params, "limit": limit, "offset": offset}
                response = self.client.post(url, headers=self.headers, json=json_body)

            if response.status_code != 200:
                raise Exception(f"HTTP {response.status_code} error: {response.text}")

            response_data = response.json()
            if "data" not in response_data:
                raise Exception(
                    "Attio API returned a response without the expected data"
                )

            data = response_data["data"]
            for item in data:
                flat_item = flatten_item(item)
                yield flat_item
            if len(data) < limit:
                break

            offset += limit

    def fetch_all(self, path: str, method: str = "get", params=None):
        url = f"{self.base_url}/{path}"
        params = params or {}

        if method == "get":
            response = self.client.get(url, headers=self.headers, params=params)
        else:
            response = self.client.post(url, headers=self.headers, json=params)

        response.raise_for_status()
        data = response.json().get("data", [])
        for item in data:
            yield flatten_item(item)


def flatten_item(item: dict) -> dict:
    if "id" in item:
        for key, value in item["id"].items():
            item[key] = value
    return item
