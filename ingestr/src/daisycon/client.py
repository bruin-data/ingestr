"""Minimal Daisycon API client with OAuth refresh."""

from typing import Any, Dict, Iterable

from dlt.common import pendulum

from ..http_client import create_client


class DaisyconClient:
    """Client for the Daisycon API using OAuth2 refresh tokens."""

    def __init__(
        self,
        client_id: str,
        client_secret: str,
        refresh_token: str,
        advertiser_ids: list[str],
    ) -> None:
        self.client_id = client_id
        self.client_secret = client_secret
        self.refresh_token = refresh_token
        self.advertiser_ids = advertiser_ids
        self.base_url = "https://services.daisycon.com/advertisers"
        self.session = create_client()
        self.access_token: str | None = None

    def refresh_access_token(self) -> str:
        """Exchange refresh token for access token."""
        data = {
            "grant_type": "refresh_token",
            "refresh_token": self.refresh_token,
            "client_id": self.client_id,
            "client_secret": self.client_secret,
        }
        response = self.session.post(
            "https://login.daisycon.com/oauth/access-token", data=data
        )
        response.raise_for_status()
        token = response.json().get("access_token")
        if not token:
            raise ValueError("Could not obtain access token")
        self.access_token = token
        return token

    def _get(
        self, advertiser_id: str, endpoint: str, params: Dict[str, Any] | None = None
    ) -> list[Dict[str, Any]]:
        if self.access_token is None:
            self.refresh_access_token()
        headers = {"Authorization": f"Bearer {self.access_token}"}
        url = f"{self.base_url}/{advertiser_id}{endpoint}"
        response = self.session.get(url, headers=headers, params=params)
        if response.status_code == 401:
            self.refresh_access_token()
            headers = {"Authorization": f"Bearer {self.access_token}"}
            response = self.session.get(url, headers=headers, params=params)
        response.raise_for_status()
        return response.json()

    def _paginated_transactions(
        self,
        advertiser_id: str,
        start_date: str,
        end_date: str,
        per_page: int,
        currency_code: str,
    ) -> Iterable[Dict[str, Any]]:
        page = 1
        while True:
            params = {
                "date_modified_start": start_date,
                "date_modified_end": end_date,
                "page": page,
                "per_page": per_page,
                "currency_code": currency_code,
            }
            records = self._get(advertiser_id, "/transactions", params=params)

            for record in records:
                if "parts" in record:
                    for part in record["parts"]:
                        flattened_record = {**record}
                        if "parts" in flattened_record:
                            del flattened_record["parts"]
                        flattened_record.update(part)

                        if "last_modified" in flattened_record:
                            try:
                                dt = pendulum.parse(
                                    str(flattened_record["last_modified"])
                                )
                                flattened_record["last_modified"] = dt.in_tz("UTC")  # type: ignore
                            except Exception:
                                raise ValueError(
                                    "Failed to parse last_modified timestamp..."
                                )

                        yield flattened_record
            if len(records) < per_page:
                break
            page += 1

    def paginated_transactions(
        self, start_date: str, end_date: str, currency_code: str, per_page: int = 1000
    ) -> Iterable[Dict[str, Any]]:
     
        for advertiser_id in self.advertiser_ids:
            yield from self._paginated_transactions(
                advertiser_id, start_date, end_date, per_page, currency_code
            )
