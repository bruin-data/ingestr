import requests

BASE_URL = "https://api.customer.io"


class CustomerIoClient:
    def __init__(self, api_key: str):
        self.api_key = api_key

    def _get_headers(self):
        return {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

    def _fetch_pages(
        self,
        session: requests.Session,
        url: str,
        params: dict | None = None,
        data_key: str = "activities",
    ) -> list:
        all_items = []
        if params is None:
            params = {}

        while True:
            response = session.get(
                url=url, headers=self._get_headers(), params=params
            )
            response.raise_for_status()
            result = response.json()

            items = result.get(data_key, [])
            all_items.extend(items)

            next_token = result.get("next")
            if next_token is None:
                break

            params["start"] = next_token

        return all_items

    def fetch_activities(
        self,
        session: requests.Session,
        activity_type: str | None = None,
        name: str | None = None,
        customer_id: str | None = None,
        id_type: str | None = None,
        deleted: bool = False,
        limit: int = 100,
    ):
        url = f"{BASE_URL}/v1/activities"
        params = {"limit": limit, "deleted": str(deleted).lower()}

        if activity_type:
            params["type"] = activity_type
        if name:
            params["name"] = name
        if customer_id:
            params["customer_id"] = customer_id
        if id_type:
            params["id_type"] = id_type

        return self._fetch_pages(session, url, params, data_key="activities")

    def fetch_broadcasts(self, session: requests.Session):
        url = f"{BASE_URL}/v1/broadcasts"
        return self._fetch_pages(session, url, data_key="broadcasts")

    def fetch_campaigns(self, session: requests.Session):
        url = f"{BASE_URL}/v1/campaigns"
        return self._fetch_pages(session, url, data_key="campaigns")

    def fetch_broadcast_metrics(
        self,
        session: requests.Session,
        period: str = "days",
        metric_type: str | None = None,
    ) -> list:
        max_steps = {
            "hours": 24,
            "days": 45,
            "weeks": 12,
            "months": 12,
        }
        steps = max_steps.get(period, 45)

        broadcasts = self.fetch_broadcasts(session)
        all_metrics = []

        for broadcast in broadcasts:
            broadcast_id = broadcast.get("id")
            url = f"{BASE_URL}/v1/broadcasts/{broadcast_id}/metrics"
            params = {"period": period, "steps": steps}

            if metric_type:
                params["type"] = metric_type

            response = session.get(url=url, headers=self._get_headers(), params=params)
            response.raise_for_status()
            result = response.json()

            for i, metric in enumerate(result.get("series", {}).get("series", [])):
                metric["broadcast_id"] = broadcast_id
                metric["period"] = period
                metric["step_index"] = i
                if metric_type:
                    metric["metric_type"] = metric_type
                all_metrics.append(metric)

        return all_metrics

    def fetch_broadcast_actions(
        self, session: requests.Session, broadcast_id: int
    ) -> list:
        url = f"{BASE_URL}/v1/broadcasts/{broadcast_id}/actions"

        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()

        actions = []
        for action in result.get("actions", []):
            action["broadcast_id"] = broadcast_id
            actions.append(action)

        return actions

    def fetch_broadcast_messages(
        self,
        session: requests.Session,
        broadcast_id: int,
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        url = f"{BASE_URL}/v1/broadcasts/{broadcast_id}/messages"
        params = {}

        if start_ts:
            params["start_ts"] = start_ts
        if end_ts:
            params["end_ts"] = end_ts

        return self._fetch_pages(session, url, params, data_key="messages")

    def fetch_broadcast_action_metrics(
        self,
        session: requests.Session,
        broadcast_id: int,
        action_id: int,
        period: str = "days",
    ) -> list:
        max_steps = {
            "hours": 24,
            "days": 45,
            "weeks": 12,
            "months": 12,
        }
        steps = max_steps.get(period, 45)

        url = f"{BASE_URL}/v1/broadcasts/{broadcast_id}/actions/{action_id}/metrics"
        params = {"period": period, "steps": steps}

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        result = response.json()

        metrics = []
        for i, metric in enumerate(result.get("series", {}).get("series", [])):
            metric["broadcast_id"] = broadcast_id
            metric["action_id"] = action_id
            metric["period"] = period
            metric["step_index"] = i
            metrics.append(metric)

        return metrics

    def fetch_campaign_metrics(
        self,
        session: requests.Session,
        campaign_id: int,
        period: str = "days",
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        url = f"{BASE_URL}/v1/campaigns/{campaign_id}/metrics"
        params = {"version": 2, "res": period}

        if start_ts:
            params["start"] = start_ts
        if end_ts:
            params["end"] = end_ts

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        result = response.json()

        metrics = []
        for i, metric in enumerate(result.get("series", {}).get("series", [])):
            metric["campaign_id"] = campaign_id
            metric["period"] = period
            metric["step_index"] = i
            metrics.append(metric)

        return metrics

    def fetch_campaign_actions(
        self, session: requests.Session, campaign_id: int
    ) -> list:
        url = f"{BASE_URL}/v1/campaigns/{campaign_id}/actions"
        actions = self._fetch_pages(session, url, data_key="actions")
        for action in actions:
            action["campaign_id"] = campaign_id
        return actions

    def fetch_campaign_action_metrics(
        self,
        session: requests.Session,
        campaign_id: int,
        action_id: int,
        period: str = "days",
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        url = f"{BASE_URL}/v1/campaigns/{campaign_id}/actions/{action_id}/metrics"
        params = {"version": 2, "res": period}

        if start_ts:
            params["start"] = start_ts
        if end_ts:
            params["end"] = end_ts

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        result = response.json()

        metrics = []
        for i, metric in enumerate(result.get("series", {}).get("series", [])):
            metric["campaign_id"] = campaign_id
            metric["action_id"] = action_id
            metric["period"] = period
            metric["step_index"] = i
            metrics.append(metric)

        return metrics
