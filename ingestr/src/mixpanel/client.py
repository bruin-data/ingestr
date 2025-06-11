import json
from typing import Iterable, Optional

import pendulum
import requests
from dlt.sources.helpers.requests import Client


class MixpanelClient:
    def __init__(self, username: str, password: str, project_id: str):
        self.username = username
        self.password = password
        self.project_id = project_id
        self.session = Client(raise_for_status=False).session

    def fetch_events(self, start_date: pendulum.DateTime, end_date: pendulum.DateTime) -> Iterable[dict]:
        url = "https://data-eu.mixpanel.com/api/2.0/export/"
        params = {
            "project_id": self.project_id,
            "from_date": start_date.format("2025-06-09"),
            "to_date": end_date.format("2025-06-11"),
        }
        headers = {
            "accept": "text/plain",
        }
        from requests.auth import HTTPBasicAuth
        auth = HTTPBasicAuth(self.username, self.password)
        resp = self.session.get(url, params=params, headers=headers, auth=auth)
        print("resp", resp.text)
        print("resp", resp)
        resp.raise_for_status()
        for line in resp.iter_lines():
            if line:
                yield json.loads(line.decode())

    def fetch_profiles(self, last_seen: Optional[pendulum.DateTime] = None) -> Iterable[dict]:
        url = "https://mixpanel.com/api/2.0/engage"
        page = 0
        session_id = None
        while True:
            params = {"page": page}
            if session_id:
                params["session_id"] = session_id
            if last_seen is not None:
                where = f'properties["$last_seen"] >= "{last_seen.to_date_string()}"'
                params["where"] = where
            resp = self.session.get(url, params=params, auth=(self.api_secret, ""))
            resp.raise_for_status()
            data = resp.json()
            results = data.get("results", [])
            if not results:
                break
            session_id = data.get("session_id", session_id)
            for item in results:
                yield item
            page += 1
