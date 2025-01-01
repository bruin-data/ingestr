import json
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


