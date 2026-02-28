import logging
from typing import Any, Dict, Generator, List, Optional

import requests

from ingestr.src.http_client import create_client

logger = logging.getLogger(__name__)

SPOTIFY_TOKEN_URL = "https://accounts.spotify.com/api/token"
SPOTIFY_API_BASE_URL = "https://api.spotify.com/v1"

SEARCH_TYPE_MAP = {
    "albums": "album",
    "artists": "artist",
    "audiobooks": "audiobook",
    "episodes": "episode",
    "playlists": "playlist",
    "shows": "show",
    "tracks": "track",
}


class SpotifyClient:
    def __init__(self, client_id: str, client_secret: str):
        self.client_id = client_id
        self.client_secret = client_secret
        self._access_token: Optional[str] = None
        self.client = create_client(retry_status_codes=[429, 502, 503, 504])

    def _authenticate(self) -> str:
        if self._access_token:
            return self._access_token

        response = self.client.post(
            SPOTIFY_TOKEN_URL,
            data={
                "grant_type": "client_credentials",
                "client_id": self.client_id,
                "client_secret": self.client_secret,
            },
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )

        if response.status_code != 200:
            raise ValueError(
                f"Spotify authentication failed (HTTP {response.status_code}): "
                f"{response.text}"
            )

        data = response.json()
        self._access_token = data["access_token"]
        return self._access_token

    def _get_headers(self) -> Dict[str, str]:
        token = self._authenticate()
        return {"Authorization": f"Bearer {token}"}

    def _request(
        self, path: str, params: Optional[Dict[str, Any]] = None
    ) -> requests.Response:
        url = f"{SPOTIFY_API_BASE_URL}/{path}"
        response = self.client.get(url, headers=self._get_headers(), params=params)

        if response.status_code == 401:
            self._access_token = None
            response = self.client.get(url, headers=self._get_headers(), params=params)

        if response.status_code != 200:
            raise ValueError(
                f"Spotify API error (HTTP {response.status_code}): {response.text}"
            )

        return response

    def fetch_all(
        self,
        query: str,
        search_type: str,
        result_key: str,
        market: str = "US",
        limit: int = 10,
    ) -> Generator[List[Dict[str, Any]], None, None]:
        offset = 0
        max_offset = 1000

        while offset < max_offset:
            params: Dict[str, Any] = {
                "q": query,
                "type": search_type,
                "market": market,
                "limit": limit,
                "offset": offset,
                "include_external": "audio",
            }

            data = self._request("search", params=params).json()
            result = data.get(result_key, {})
            items = result.get("items", [])

            if not items:
                break

            yield items

            total = result.get("total", 0)
            offset += limit
            if offset >= total:
                break
