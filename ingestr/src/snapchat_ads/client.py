import requests
from dlt.sources.helpers.requests import Client


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session


class SnapchatAdsAPI:
    """Helper class for Snapchat Ads API authentication and requests."""

    TOKEN_URL = "https://accounts.snapchat.com/login/oauth2/access_token"

    def __init__(self, refresh_token: str, client_id: str, client_secret: str):
        self.refresh_token = refresh_token
        self.client_id = client_id
        self.client_secret = client_secret
        self._access_token = None

    def get_access_token(self) -> str:
        """
        Refresh the access token using the refresh token.

        Returns:
            str: The access token
        """
        if self._access_token:
            return self._access_token

        client = create_client()
        response = client.post(
            self.TOKEN_URL,
            data={
                "refresh_token": self.refresh_token,
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "grant_type": "refresh_token",
            },
        )

        if response.status_code != 200:
            raise ValueError(
                f"Failed to refresh access token: {response.status_code} - {response.text}"
            )

        result = response.json()
        self._access_token = result.get("access_token")

        if not self._access_token:
            raise ValueError(f"No access token in response: {result}")

        return self._access_token

    def get_headers(self) -> dict:
        access_token = self.get_access_token()
        return {
            "Authorization": f"Bearer {access_token}",
            "Content-Type": "application/json",
        }
