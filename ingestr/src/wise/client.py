from typing import Iterable

import pendulum
from dlt.sources.helpers.requests import Client


class WiseClient:
    BASE_URL = "https://api.transferwise.com"

    def __init__(self, api_key: str) -> None:
        self.session = Client(raise_for_status=False).session
        self.session.headers.update({"Authorization": f"Bearer {api_key}"})

    #https://docs.wise.com/api-docs/api-reference/profile#list-profiles
    def fetch_profiles(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v2/profiles"
        resp = self.session.get(url)
        resp.raise_for_status()
        for profile in resp.json():
            yield profile
    
    #https://docs.wise.com/api-docs/api-reference/transfer#list-transfers
    def fetch_transfers(self, profile_id: str, start_time=pendulum.DateTime, end_time=pendulum.DateTime):
        offset = 0
     
        while True:
            data = self.session.get(
                f"{self.BASE_URL}/v1/transfers",
              
                params={
                    "profile": profile_id,
                    "createdDateStart": start_time.to_date_string(),
                    "createdDateEnd": end_time.to_date_string(),
                    "limit": 100,
                    "offset": offset
                }
            )
            response_data = data.json()
            
            if not response_data or len(response_data) == 0:
                break
            
            for transfer in response_data:
                transfer['created'] = pendulum.parse(transfer['created'])
            
                yield transfer
            offset += 100
