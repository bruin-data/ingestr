import time
from typing import Any, Dict, Iterator, Optional

import pendulum

from ingestr.src.http_client import create_client


class ZoomClient:
    """Minimal Zoom API client supporting Server-to-Server OAuth."""

    def __init__(
        self,
        client_id: Optional[str] = None,
        client_secret: Optional[str] = None,
        account_id: Optional[str] = None,
    ) -> None:
        self.client_id = client_id
        self.client_secret = client_secret
        self.account_id = account_id
        self.token_expires_at: float = 0
        self.base_url = "https://api.zoom.us/v2"
        self.session = create_client()
        self._refresh_access_token()

    def _refresh_access_token(self) -> None:
        token_url = "https://zoom.us/oauth/token"
        auth = (self.client_id, self.client_secret)
        resp = self.session.post(
            token_url,
            params={"grant_type": "account_credentials", "account_id": self.account_id},
            auth=auth,
        )
        resp.raise_for_status()
        data = resp.json()
        self.access_token = data.get("access_token")
        self.token_expires_at = time.time() + data.get("expires_in", 3600)

    def _ensure_token(self) -> None:
        if self.access_token is None or self.token_expires_at <= time.time():
            self._refresh_access_token()

    def _headers(self) -> Dict[str, str]:
        self._ensure_token()
        return {
            "Authorization": f"Bearer {self.access_token}",
            "Accept": "application/json",
        }

    def get_users(self) -> Iterator[Dict[str, Any]]:
        url = f"{self.base_url}/users"

        params = {"page_size": 1000}
        while True:
            response = self.session.get(url, headers=self._headers(), params=params)
            response.raise_for_status()
            data = response.json()
            for user in data.get("users", []):
                yield user
            token = data.get("next_page_token")
            if not token:
                break
            params["next_page_token"] = token

    # https://developers.zoom.us/docs/api/rest/reference/zoom-api/methods/#operation/meetings
    def get_meetings(
        self, user_id: str, params: Dict[str, Any]
    ) -> Iterator[Dict[str, Any]]:
        url = f"{self.base_url}/users/{user_id}/meetings"
        while True:
            response = self.session.get(url, headers=self._headers(), params=params)
            response.raise_for_status()
            data = response.json()
            for item in data.get("meetings", []):
                item["zoom_user_id"] = user_id
                yield item
            token = data.get("next_page_token")
            if not token:
                break
            params["next_page_token"] = token

    # https://developers.zoom.us/docs/api/rest/reference/zoom-api/methods/#operation/reportMeetingParticipants
    def get_participants(
        self,
        meeting_id: str,
        params: Dict[str, Any],
        start_date: pendulum.DateTime,
        end_date: pendulum.DateTime,
    ) -> Iterator[Dict[str, Any]]:
        url = f"{self.base_url}/report/meetings/{meeting_id}/participants"
        while True:
            response = self.session.get(url, headers=self._headers(), params=params)
            response.raise_for_status()
            data = response.json()
            for item in data.get("participants", []):
                join_time = pendulum.parse(item["join_time"])
                if join_time >= start_date and join_time <= end_date:
                    yield item
            token = data.get("next_page_token")
            if not token:
                break
            params["next_page_token"] = token
