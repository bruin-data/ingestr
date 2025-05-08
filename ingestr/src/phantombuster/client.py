from typing import Union

import pendulum
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
        self,
        session: requests.Session,
        agent_id: str,
        start_date: pendulum.DateTime,
        end_date: pendulum.DateTime,
    ):
        url = "https://api.phantombuster.com/api/v2/containers/fetch-all/"
        before_ended_at = None
        limit = 100

        started_at = start_date.int_timestamp * 1000 + int(
            start_date.microsecond / 1000
        )
        ended_at = end_date.int_timestamp * 1000 + int(end_date.microsecond / 1000)

        while True:
            params: dict[str, Union[str, int, float, bytes, None]] = {
                "agentId": agent_id,
                "limit": limit,
                "mode": "finalized",
            }

            if before_ended_at:
                params["beforeEndedAt"] = before_ended_at

            response = session.get(url=url, headers=self._get_headers(), params=params)
            data = response.json()
            containers = data.get("containers", [])

            for container in containers:
                container_ended_at = container.get("endedAt")

                if before_ended_at is None or before_ended_at > container_ended_at:
                    before_ended_at = container_ended_at

                if container_ended_at < started_at or container_ended_at > ended_at:
                    continue

                try:
                    result = self.fetch_result_object(session, container["id"])
                    partition_dt = pendulum.from_timestamp(
                        container_ended_at / 1000, tz="UTC"
                    ).date()
                    container_ended_at_datetime = pendulum.from_timestamp(
                        container_ended_at / 1000, tz="UTC"
                    )
                    row = {
                        "container_id": container["id"],
                        "container": container,
                        "result": result,
                        "partition_dt": partition_dt,
                        "ended_at": container_ended_at_datetime,
                    }
                    yield row

                except requests.RequestException as e:
                    print(f"Error fetching result for container {container['id']}: {e}")

            if data["maxLimitReached"] is False:
                break

    def fetch_result_object(self, session: requests.Session, container_id: str):
        result_url = (
            "https://api.phantombuster.com/api/v2/containers/fetch-result-object"
        )
        params = {"id": container_id}
        response = session.get(result_url, headers=self._get_headers(), params=params)
        response.raise_for_status()

        return response.json()
