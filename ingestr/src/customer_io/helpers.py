from typing import Any

import requests

BASE_URLS = {
    "us": "https://api.customer.io",
    "eu": "https://api-eu.customer.io",
}

# Maximum steps for each metrics period type
MAX_STEPS_BY_PERIOD = {
    "hours": 24,
    "days": 45,
    "weeks": 12,
    "months": 12,
}

# For newsletter metrics, months has a different max
MAX_STEPS_NEWSLETTER = {
    "hours": 24,
    "days": 45,
    "weeks": 12,
    "months": 121,
}


class CustomerIoClient:
    def __init__(self, api_key: str, region: str = "us"):
        self.api_key = api_key
        self.base_url = BASE_URLS.get(region.lower(), BASE_URLS["us"])

    def _get_headers(self):
        return {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

    def _extract_metrics(
        self,
        result: dict,
        period: str,
        extra_fields: dict | None = None,
    ) -> list:
        """Extract metrics from API response and add common fields."""
        metrics = []
        for i, metric in enumerate(result.get("series", {}).get("series", [])):
            metric["period"] = period
            metric["step_index"] = i
            if extra_fields:
                metric.update(extra_fields)
            metrics.append(metric)
        return metrics

    def _fetch_pages(
        self,
        session: requests.Session,
        url: str,
        params: dict[str, Any] | None = None,
        data_key: str = "activities",
    ) -> list:
        all_items: list[Any] = []
        if params is None:
            params = {}

        while True:
            response = session.get(url=url, headers=self._get_headers(), params=params)
            response.raise_for_status()
            result = response.json()

            items = result.get(data_key) or []
            all_items.extend(items)

            next_token = result.get("next")
            if not next_token:
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
        url = f"{self.base_url}/v1/activities"
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
        url = f"{self.base_url}/v1/broadcasts"
        return self._fetch_pages(session, url, data_key="broadcasts")

    def fetch_campaigns(self, session: requests.Session):
        url = f"{self.base_url}/v1/campaigns"
        return self._fetch_pages(session, url, data_key="campaigns")

    def fetch_collections(self, session: requests.Session):
        url = f"{self.base_url}/v1/collections"
        return self._fetch_pages(session, url, data_key="collections")

    def fetch_exports(self, session: requests.Session):
        url = f"{self.base_url}/v1/exports"
        return self._fetch_pages(session, url, data_key="exports")

    def fetch_info_ip_addresses(self, session: requests.Session):
        url = f"{self.base_url}/v1/info/ip_addresses"
        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()
        ips = result.get("ips", [])
        return [{"ip": ip} for ip in ips]

    def fetch_messages(
        self,
        session: requests.Session,
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        url = f"{self.base_url}/v1/messages"
        params = {"limit": 1000}

        if start_ts:
            params["start_ts"] = start_ts
        if end_ts:
            params["end_ts"] = end_ts

        return self._fetch_pages(session, url, params, data_key="messages")

    def fetch_newsletters(self, session: requests.Session) -> list:
        url = f"{self.base_url}/v1/newsletters"
        params = {"limit": 100}
        return self._fetch_pages(session, url, params, data_key="newsletters")

    def fetch_reporting_webhooks(self, session: requests.Session) -> list:
        url = f"{self.base_url}/v1/reporting_webhooks"
        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()
        return result.get("reporting_webhooks") or []

    def fetch_segments(self, session: requests.Session) -> list:
        url = f"{self.base_url}/v1/segments"
        return self._fetch_pages(session, url, data_key="segments")

    def fetch_transactional_messages(self, session: requests.Session) -> list:
        url = f"{self.base_url}/v1/transactional"
        return self._fetch_pages(session, url, data_key="transactional")

    def fetch_workspaces(self, session: requests.Session) -> list:
        url = f"{self.base_url}/v1/workspaces"
        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()
        return result.get("workspaces", [])

    def fetch_newsletter_test_groups(
        self, session: requests.Session, newsletter_id: int
    ) -> list:
        url = f"{self.base_url}/v1/newsletters/{newsletter_id}/test_groups"
        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()

        groups = []
        for group in result.get("test_groups", []):
            group["newsletter_id"] = newsletter_id
            groups.append(group)

        return groups

    def fetch_newsletter_metrics(
        self,
        session: requests.Session,
        newsletter_id: int,
        period: str = "days",
    ) -> list:
        steps = MAX_STEPS_NEWSLETTER.get(period, 45)
        url = f"{self.base_url}/v1/newsletters/{newsletter_id}/metrics"
        params: dict[str, Any] = {"period": period, "steps": steps}

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        return self._extract_metrics(
            response.json(), period, {"newsletter_id": newsletter_id}
        )

    def fetch_broadcast_metrics(
        self,
        session: requests.Session,
        period: str = "days",
    ) -> list:
        steps = MAX_STEPS_BY_PERIOD.get(period, 45)
        broadcasts = self.fetch_broadcasts(session)
        all_metrics = []

        for broadcast in broadcasts:
            broadcast_id = broadcast.get("id")
            url = f"{self.base_url}/v1/broadcasts/{broadcast_id}/metrics"
            params: dict[str, Any] = {"period": period, "steps": steps}

            response = session.get(url=url, headers=self._get_headers(), params=params)
            response.raise_for_status()
            all_metrics.extend(
                self._extract_metrics(
                    response.json(), period, {"broadcast_id": broadcast_id}
                )
            )

        return all_metrics

    def fetch_broadcast_actions(
        self, session: requests.Session, broadcast_id: int
    ) -> list:
        url = f"{self.base_url}/v1/broadcasts/{broadcast_id}/actions"

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
        url = f"{self.base_url}/v1/broadcasts/{broadcast_id}/messages"
        params: dict[str, Any] = {}

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
        steps = MAX_STEPS_BY_PERIOD.get(period, 45)
        url = (
            f"{self.base_url}/v1/broadcasts/{broadcast_id}/actions/{action_id}/metrics"
        )
        params: dict[str, Any] = {"period": period, "steps": steps}

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        return self._extract_metrics(
            response.json(),
            period,
            {"broadcast_id": broadcast_id, "action_id": action_id},
        )

    def fetch_campaign_metrics(
        self,
        session: requests.Session,
        campaign_id: int,
        period: str = "days",
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        url = f"{self.base_url}/v1/campaigns/{campaign_id}/metrics"
        params: dict[str, Any] = {"version": 2, "res": period}

        if start_ts:
            params["start"] = start_ts
        if end_ts:
            params["end"] = end_ts

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        return self._extract_metrics(
            response.json(), period, {"campaign_id": campaign_id}
        )

    def fetch_campaign_actions(
        self, session: requests.Session, campaign_id: int
    ) -> list:
        url = f"{self.base_url}/v1/campaigns/{campaign_id}/actions"
        actions = self._fetch_pages(session, url, data_key="actions")
        for action in actions:
            action["campaign_id"] = campaign_id
        return actions

    def fetch_sender_identities(self, session: requests.Session) -> list:
        url = f"{self.base_url}/v1/sender_identities"
        return self._fetch_pages(session, url, data_key="sender_identities")

    def fetch_campaign_action_metrics(
        self,
        session: requests.Session,
        campaign_id: int,
        action_id: int,
        period: str = "days",
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        url = f"{self.base_url}/v1/campaigns/{campaign_id}/actions/{action_id}/metrics"
        params: dict[str, Any] = {"version": 2, "res": period}

        if start_ts:
            params["start"] = start_ts
        if end_ts:
            params["end"] = end_ts

        response = session.get(url=url, headers=self._get_headers(), params=params)
        response.raise_for_status()
        return self._extract_metrics(
            response.json(),
            period,
            {"campaign_id": campaign_id, "action_id": action_id},
        )

    def fetch_customers(
        self,
        session: requests.Session,
        limit: int = 1000,
    ) -> list:
        """Fetch customers by iterating through all segments and getting their members."""
        # The POST /v1/customers endpoint requires a filter, so we fetch segment members instead
        all_customers = {}
        segments = self.fetch_segments(session)

        for segment in segments:
            segment_id = segment.get("id")
            if segment_id:
                members = self.fetch_segment_members(session, segment_id, limit)
                for member in members:
                    # Use cio_id as unique key to deduplicate across segments
                    cio_id = member.get("cio_id")
                    if cio_id and cio_id not in all_customers:
                        all_customers[cio_id] = member

        return list(all_customers.values())

    def fetch_segment_members(
        self,
        session: requests.Session,
        segment_id: int,
        limit: int = 1000,
    ) -> list:
        """Fetch members of a specific segment."""
        url = f"{self.base_url}/v1/segments/{segment_id}/membership"
        params = {"limit": limit}
        return self._fetch_pages(session, url, params, data_key="identifiers")

    def _fetch_pages_post(
        self,
        session: requests.Session,
        url: str,
        params: dict[str, Any] | None = None,
        data_key: str = "identifiers",
    ) -> list:
        """Pagination helper for POST requests."""
        all_items: list[Any] = []
        if params is None:
            params = {}
        query_params: dict[str, Any] = {}

        while True:
            response = session.post(
                url=url, headers=self._get_headers(), json=params, params=query_params
            )
            response.raise_for_status()
            result = response.json()

            items = result.get(data_key) or []
            all_items.extend(items)

            next_token = result.get("next")
            if not next_token:
                break

            query_params["start"] = next_token

        return all_items

    def fetch_customer_attributes(
        self, session: requests.Session, customer_id: str
    ) -> dict | None:
        """Fetch attributes for a specific customer."""
        url = f"{self.base_url}/v1/customers/{customer_id}/attributes"
        response = session.get(url=url, headers=self._get_headers())
        if response.status_code == 404:
            return None
        response.raise_for_status()
        result = response.json()
        customer = result.get("customer", {})
        customer["customer_id"] = customer_id
        return customer

    def fetch_customer_messages(
        self,
        session: requests.Session,
        customer_id: str,
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        """Fetch messages sent to a specific customer."""
        url = f"{self.base_url}/v1/customers/{customer_id}/messages"
        params = {}
        if start_ts:
            params["start_ts"] = start_ts
        if end_ts:
            params["end_ts"] = end_ts

        messages = self._fetch_pages(session, url, params, data_key="messages")
        for msg in messages:
            msg["customer_id"] = customer_id
        return messages

    def fetch_customer_activities(
        self, session: requests.Session, customer_id: str
    ) -> list:
        """Fetch activities for a specific customer."""
        url = f"{self.base_url}/v1/customers/{customer_id}/activities"
        activities = self._fetch_pages(session, url, data_key="activities")
        for activity in activities:
            activity["customer_id"] = customer_id
        return activities

    def fetch_customer_relationships(
        self, session: requests.Session, customer_id: str
    ) -> list:
        """Fetch object relationships for a specific customer."""
        url = f"{self.base_url}/v1/customers/{customer_id}/relationships"
        relationships = self._fetch_pages(session, url, data_key="cio_relationships")
        for rel in relationships:
            rel["customer_id"] = customer_id
        return relationships

    def fetch_object_types(self, session: requests.Session) -> list:
        """Fetch all object types in the workspace."""
        url = f"{self.base_url}/v1/object_types"
        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()
        return result.get("types", [])

    def fetch_objects(
        self, session: requests.Session, object_type_id: str, limit: int = 1000
    ) -> list:
        """Fetch objects of a specific type."""
        url = f"{self.base_url}/v1/objects"
        params = {"object_type_id": object_type_id, "limit": limit}
        objects = self._fetch_pages_post(session, url, params, data_key="identifiers")
        for obj in objects:
            obj["object_type_id"] = object_type_id
        return objects

    def fetch_object_attributes(
        self, session: requests.Session, object_type_id: str, object_id: str
    ) -> dict | None:
        """Fetch attributes for a specific object."""
        url = f"{self.base_url}/v1/objects/{object_type_id}/{object_id}/attributes"
        response = session.get(url=url, headers=self._get_headers())
        if response.status_code == 404:
            return None
        response.raise_for_status()
        result = response.json()
        obj = result.get("object", {})
        obj["object_type_id"] = object_type_id
        obj["object_id"] = object_id
        return obj

    def fetch_object_relationships(
        self, session: requests.Session, object_type_id: str, object_id: str
    ) -> list:
        """Fetch people related to a specific object."""
        url = f"{self.base_url}/v1/objects/{object_type_id}/{object_id}/relationships"
        relationships = self._fetch_pages(session, url, data_key="cio_relationships")
        for rel in relationships:
            rel["object_type_id"] = object_type_id
            rel["object_id"] = object_id
        return relationships

    def fetch_campaign_messages(
        self,
        session: requests.Session,
        campaign_id: int,
        start_ts: int | None = None,
        end_ts: int | None = None,
    ) -> list:
        """Fetch messages/deliveries for a specific campaign."""
        url = f"{self.base_url}/v1/campaigns/{campaign_id}/messages"
        params = {}
        if start_ts:
            params["start_ts"] = start_ts
        if end_ts:
            params["end_ts"] = end_ts

        messages = self._fetch_pages(session, url, params, data_key="messages")
        for msg in messages:
            msg["campaign_id"] = campaign_id
        return messages

    def fetch_subscription_topics(self, session: requests.Session) -> list:
        """Fetch subscription topics in the workspace."""
        url = f"{self.base_url}/v1/subscription_topics"
        response = session.get(url=url, headers=self._get_headers())
        response.raise_for_status()
        result = response.json()
        return result.get("topics", [])
