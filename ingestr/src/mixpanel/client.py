import json
from typing import Iterable

import pendulum
from dlt.sources.helpers.requests import Client


class MixpanelClient:
    def __init__(self, username: str, password: str, project_id: str, server: str):
        self.username = username
        self.password = password
        self.project_id = project_id
        self.server = server
        self.session = Client(raise_for_status=False).session

    def fetch_events(
        self, start_date: pendulum.DateTime, end_date: pendulum.DateTime
    ) -> Iterable[dict]:
        if self.server == "us":
            server = "data"
        elif self.server == "in":
            server = "data-in"
        else:
            server = "data-eu"

        url = f"https://{server}.mixpanel.com/api/2.0/export/"
        params = {
            "project_id": self.project_id,
            "from_date": start_date.format("YYYY-MM-DD"),
            "to_date": end_date.format("YYYY-MM-DD"),
        }
        headers = {
            "accept": "text/plain",
        }
        from requests.auth import HTTPBasicAuth

        auth = HTTPBasicAuth(self.username, self.password)
        resp = self.session.get(url, params=params, headers=headers, auth=auth)
        resp.raise_for_status()
        for line in resp.iter_lines():
            if line:
                data = json.loads(line.decode())
                if "properties" in data:
                    for key, value in data["properties"].items():
                        if key.startswith("$"):
                            data[key[1:]] = value
                        else:
                            data[key] = value
                    del data["properties"]
                yield data

    def fetch_profiles(
        self, start_date: pendulum.DateTime, end_date: pendulum.DateTime
    ) -> Iterable[dict]:
        if self.server == "us":
            server = ""
        elif self.server == "in":
            server = "in."
        else:
            server = "eu."
        url = f"https://{server}mixpanel.com/api/query/engage"
        headers = {
            "accept": "application/json",
            "content-type": "application/x-www-form-urlencoded",
        }
        from requests.auth import HTTPBasicAuth

        auth = HTTPBasicAuth(self.username, self.password)
        page = 0
        session_id = None
        while True:
            params = {"project_id": self.project_id, "page": str(page)}
            if session_id:
                params["session_id"] = session_id
            start_str = start_date.format("YYYY-MM-DDTHH:mm:ss")
            end_str = end_date.format("YYYY-MM-DDTHH:mm:ss")
            where = f'properties["$last_seen"] >= "{start_str}" and properties["$last_seen"] <= "{end_str}"'
            params["where"] = where
            resp = self.session.post(url, params=params, headers=headers, auth=auth)

            resp.raise_for_status()
            data = resp.json()

            for result in data.get("results", []):
                for key, value in result["$properties"].items():
                    if key.startswith("$"):
                        if key == "$last_seen":
                            result["last_seen"] = pendulum.parse(value)
                        else:
                            result[key[1:]] = value
                result["distinct_id"] = result["$distinct_id"]
                del result["$properties"]
                del result["$distinct_id"]
                yield result
            if not data.get("results"):
                break
            session_id = data.get("session_id", session_id)

            page += 1
