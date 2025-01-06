import time
import requests

from requests.models import PreparedRequest
import jwt

from typing import Sequence
from .models import AnalyticsReportRequestsResponse

class AppStoreConnectClient:
    def __init__(self, key: bytes, key_id: str, issuer_id: str):
        self.__key = key
        self.__key_id = key_id
        self.__issuer_id = issuer_id

    def list_analytics_reports(self, app_id) -> AnalyticsReportRequestsResponse:
        res = requests.get(f"https://api.appstoreconnect.apple.com/v1/apps/{app_id}/analyticsReportRequests", auth=self.auth)
        if res.ok:
            reports =  AnalyticsReportRequestsResponse.from_json(res.text)

    def auth(self, req: PreparedRequest) -> str:
        headers = {
            "alg": "ES256",
            "kid": self.__key_id,
        }
        payload = {
            "iss": self.__issuer_id,
            "exp": int(time.time()) + 600, 
            "aud": "appstoreconnect-v1"
        }
        req.headers["Authorization"] = jwt.encode(
            payload,
            self.__key,
            algorithm="ES256",
            headers=headers,
        )
        return req

            
            

