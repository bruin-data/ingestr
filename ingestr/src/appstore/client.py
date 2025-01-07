import time
import requests
from datetime import datetime

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
        res.raise_for_status()

        return AnalyticsReportRequestsResponse.from_json(res.text)
    
    def list_analytics_reports(self, req_id: str, report_name: Optional[str] = None):
        params = {}
        if report_name is not None:
            params["filter[name]"] = report_name
        res = requests.get(
            f"https://api.appstoreconnect.apple.com/v1/analyticsReportRequests/{req_id}/reports",
            auth=self.auth,
            params=params,
        )
        res.raise_for_status()
        
        return AnalyticsReportResponse.from_json(res.text)

    def list_report_instances(
            self,
            report_id: str,
            granularity: str = "DAILY",
    ) -> AnalyticsReportInstancesResponse:
        data = []
        url = f"https://api.appstoreconnect.apple.com/v1/analyticsReports/{report_id}/instances"
        params = {"filter[granularity]": granularity}

        while url:
            res = requests.get(url, auth=self.auth, params=params)
            res.raise_for_status()
            
            response_data = AnalyticsReportInstancesResponse.from_json(res.text)
            data.extend(response_data.data)
            
            url = response_data.links.next
            params = None  # Clear params for subsequent requests

        return AnalyticsReportInstancesResponse(
            data=data,
            links=response_data.links,
            meta=response_data.meta,
        )

    def list_report_segments(self, instance_id: str) -> AnalyticsReportSegmentsResponse:
        segments = []
        url = f"https://api.appstoreconnect.apple.com/v1/analyticsReportInstances/{instance_id}/segments"
    
        while url:
            res = requests.get(url, auth=self.auth)
            res.raise_for_status()
            
            response_data = AnalyticsReportSegmentsResponse.from_json(res.text)
            segments.extend(response_data.data)
            
            url = response_data.links.next
    
        return AnalyticsReportSegmentsResponse(data=segments, links=response_data.links, meta=response_data.meta)
            
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

