from typing import Union

import requests


class PhantombusterClient:
    def __init__(self, api_key: str):
        self.api_key = api_key

    def _get_headers(self):
        return {
            "X-Phantombuster-Key-1": self.api_key,
            "accept": "application/json",
        }

    def fetch_containers_result(
        self, session: requests.Session, url: str, agent_id: str
    ):
        before_ended_at = None
        limit = 1000
        while True:
            params: dict[str, Union[str, int, float, bytes, None]] = {
                "agentId": agent_id,
                "limit": limit,
                "mode": "all",
            }
            if before_ended_at:
                params["beforeEndedAt"] = before_ended_at
            response = session.get(url=url, headers=self._get_headers(), params=params)
            data = response.json()

            if not data.get("containers"):
                break

            containers = data.get("containers", [])
            ended_times = []
            for container in containers:
                try:
                    result = self.fetch_result_object(session, container["id"])
                    row = {"container_id": container["id"], "result": result}
                    yield row
                except requests.RequestException as e:
                    print(f"Error fetching result for container {container['id']}: {e}")

                if "endedAt" in container:
                    ended_times.append(container["endedAt"])

            if not ended_times:
                break

            before_ended_at = min(ended_times)

    def fetch_result_object(self, session: requests.Session, container_id: str):
        result_url = (
            "https://api.phantombuster.com/api/v2/containers/fetch-result-object"
        )
        params = {"id": container_id}
        response = session.get(result_url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        return response.json()
