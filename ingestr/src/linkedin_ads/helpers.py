import json
import requests
from dlt.sources.helpers.requests import Client
from datetime import datetime

def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429

def create_client() -> requests.Session:
    return Client(
        request_timeout=10.0,
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
    ).session

def flat_structure(items, pivot, time_granularity):
    for item in items:
        # Process pivotValues
        if "pivotValues" in item and item["pivotValues"]:
            item[pivot] = ", ".join(item["pivotValues"])

        # Process dateRange based on time granularity
        if "dateRange" in item:
            start_date = item["dateRange"]["start"]
            formatted_start_date = f"{start_date['year']}-{start_date['month']:02d}-{start_date['day']:02d}"
            
            if time_granularity == "daily":
                item["date"] = formatted_start_date
            elif time_granularity == "monthly":
                end_date = item["dateRange"]["end"]
                formatted_end_date = f"{end_date['year']}-{end_date['month']:02d}-{end_date['day']:02d}"
                item["start_date"] = formatted_start_date
                item["end_date"] = formatted_end_date
            else:
                raise ValueError(f"Invalid time_granularity: {time_granularity}")

    
        del item["dateRange"]
        del item["pivotValues"]

    return items


class LinkedInAdsAPI:
    def __init__(
        self,
        access_token,
        time_granularity,
        account_ids,
        dimension,
        metrics,
        interval_start,
        interval_end
    ):
        self.time_granularity:str = time_granularity
        self.account_ids:list[str] = account_ids
        self.dimension:str = dimension
        self.metrics:list[str] = metrics
        self.interval_start:str = interval_start
        self.interval_end:str = interval_end
        self.headers = {
             "Authorization": f"Bearer {access_token}",
            "Linkedin-Version": "202411",
            "X-Restli-Protocol-Version": "2.0.0"
        }
        #interval start is compulsory but end is optional
        self.start = datetime.strptime(interval_start, "%Y-%m-%d")
        if interval_end:
            self.end = datetime.strptime(interval_end, "%Y-%m-%d")
        else:
            self.end = None
    

    def fetch_pages(self):
        date_range = f"start:(year:{self.start.year},month:{self.start.month},day:{self.start.day})"
        if self.interval_end:
            date_range += f",end:(year:{self.end.year},month:{self.end.month},day:{self.end.day})"
        accounts = ",".join([f"urn:li:sponsoredAccount:{account_id}" for account_id in self.account_ids])
        
        params = {
        "q": "analytics",
        "timeGranularity": self.time_granularity,
        "dateRange": date_range,
        "accounts": f"List({accounts})",
        "pivot": self.dimension,
        "fields": self.metrics
        }
    
        base_url = "https://api.linkedin.com/rest/adAnalytics"
        client = create_client()
        
        while base_url:
            response = client.get(url=base_url, headers=self.headers, params=params)

            result = response.json()
            print(result)
            items = result.get("elements", [])
            
            if not items:
                raise ValueError("No items found")

            flat_structure(items=items, pivot=self.dimension, time_granularity=self.time_granularity)

            yield items

            next_link = None
            for link in result.get("paging", {}).get("links", []):
                if link.get("rel") == "next":
                    next_link = link.get("href")
                    break
            print(next_link)
            
            base_url = next_link
            params = None
            if not base_url:
                break

           
