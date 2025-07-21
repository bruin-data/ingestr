from typing import Iterable, Optional

from ..http_client import create_client


class ClickupClient:
    def __init__(self, api_token: str):
        self.session = create_client()
        self.base_url = "https://api.clickup.com/api/v2"
        self.headers = {"Authorization": api_token}

    def get(self, endpoint: str, params: Optional[dict] = None) -> dict:
        url = f"{self.base_url}{endpoint}"
        resp = self.session.get(url, headers=self.headers, params=params or {})
        resp.raise_for_status()
        return resp.json()

    def paginated(
        self, endpoint: str, key: str, params: Optional[dict] = None
    ) -> Iterable[dict]:
        page = 0
        params = params or {}
        while True:
            params["page"] = page
            data = self.get(endpoint, params)
            items = data.get(key, data)
            if not items:
                break
            for item in items:
                yield item
            if data.get("last_page") or len(items) < params.get("page_size", 100):
                break
            page += 1

    def get_teams(self):
        data = self.get("/team")
        return data.get("teams", [])

    def get_spaces(self):
        for team in self.get_teams():
            for space in self.paginated(f"/team/{team['id']}/space", "spaces"):
                yield space

    def get_lists(self):
        for space in self.get_spaces():
            for lst in self.paginated(f"/space/{space['id']}/list", "lists"):
                yield lst
