import time
import requests

from requests.models import PreparedRequest
import jwt

from typing import Sequence, Optional
from .models import (
    AnalyticsReportRequestsResponse,
    AnalyticsReportResponse,
    AnalyticsReportInstancesResponse,
    AnalyticsReportSegmentsResponse,
)

class AppStoreConnectClient:
    def __init__(self, key: bytes, key_id: str, issuer_id: str):
        self.__key = key
        self.__key_id = key_id
        self.__issuer_id = issuer_id

    def list_analytics_report_requests(self, app_id) -> AnalyticsReportRequestsResponse:
        res = requests.get(
            f"https://api.appstoreconnect.apple.com/v1/apps/{app_id}/analyticsReportRequests",
            auth=self.auth
        )
        if res.status_code != 200:
            raise Exception(f"http status: {res.status_code}")

        return AnalyticsReportRequestsResponse.from_json(res.text)
    
    def list_analytics_reports(self, req_id: str, report_type: Optional[str] = None):
        params = {}
        if report_type is not None:
            params["filter[name]"] = report_type
        res = requests.get(
            f"https://api.appstoreconnect.apple.com/v1/analyticsReportRequests/{req_id}/reports",
            auth=self.auth,
            params=params,
        )
        if res.status_code != 200:
            raise Exception(f"http status: {res.status_code}")
        
        return AnalyticsReportResponse.from_json(res.text)

    def list_report_instances(self, report_id: str):
        res = requests.get(
            f"https://api.appstoreconnect.apple.com/v1/analyticsReports/{report_id}/instances",
            auth=self.auth,
            params={
                "filter[granularity]": "DAILY"
            }
        )
        if res.status_code != 200:
            raise Exception(f"http status: {res.status_code}")

        return AnalyticsReportInstancesResponse.from_json(res.text)

    def list_report_segments(self, instance_id: str):
        res = requests.get(
            f"https://api.appstoreconnect.apple.com/v1/analyticsReportInstances/{instance_id}/segments",
            auth=self.auth,
        )
        if res.status_code != 200:
            raise Exception(f"http status: {res.status_code}")

        return AnalyticsReportSegmentsResponse.from_json(res.text)
            
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

            
            

