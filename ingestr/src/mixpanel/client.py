import json
from typing import Iterable, Optional

import pendulum
from dlt.sources.helpers.requests import Client


class MixpanelClient:
    def __init__(self, username: str, password: str, project_id: str):
        self.username = username
        self.password = password
        self.project_id = project_id
        self.session = Client(raise_for_status=False).session

    def fetch_events(
        self, start_date: pendulum.DateTime, end_date: pendulum.DateTime
    ) -> Iterable[dict]:
        url = "https://data-eu.mixpanel.com/api/2.0/export/"
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
                    data["time"] = data["properties"]["time"]
                    data["distinct_id"] = data["properties"]["distinct_id"]
                yield data

    def fetch_profiles(
        self, last_seen: Optional[pendulum.DateTime] = None
    ) -> Iterable[dict]:
        url = "https://eu.mixpanel.com/api/query/engage"
        
        headers = {
            "accept": "application/json",
            "content-type": "application/x-www-form-urlencoded",
        }
        from requests.auth import HTTPBasicAuth
        auth = HTTPBasicAuth(self.username, self.password)
        page = 0
        session_id = None
        while True:
            params = {"project_id": self.project_id, "page": page }
            if session_id:
                params["session_id"] = session_id
            # if last_seen is not None:
                # where = f'properties["$last_seen"] >= "{last_seen.to_date_string()}"'
                # params["where"] = where
            resp = self.session.post(url, params=params, headers=headers, auth=auth)
            
            resp.raise_for_status()
            data = resp.json()
            results = data.get("results", [])
            for result in results:
                if result["$properties"]:
                    result["last_seen"] = result["$properties"]["$last_seen"]
                    result["distinct_id"] = result["$distinct_id"]
                    
            if not results:
                break
            session_id = data.get("session_id", session_id)
            for item in results:
                yield item
            page += 1

