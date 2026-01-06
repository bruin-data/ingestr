import csv
import io
import time
from datetime import datetime, timedelta
from typing import Any, Dict, Iterator, Optional

import requests

INDEED_TOKEN_URL = "https://apis.indeed.com/oauth/v2/tokens"
INDEED_API_BASE_URL = "https://apis.indeed.com/ads/v1"

DEFAULT_SCOPES = [
    "employer.advertising.campaign.read",
    "employer.advertising.campaign_report.read",
    "employer.advertising.account.read",
]


def _get_oauth_token(
    client_id: str,
    client_secret: str,
    employer_id: str,
) -> str:
    response = requests.post(
        INDEED_TOKEN_URL,
        data={
            "client_id": client_id,
            "client_secret": client_secret,
            "grant_type": "client_credentials",
            "scope": " ".join(DEFAULT_SCOPES),
            "employer": employer_id,
        },
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    response.raise_for_status()
    payload = response.json()

    if "access_token" not in payload:
        raise ValueError(f"Token response missing access_token: {payload}")

    return payload["access_token"]


def _api_request(
    token: str,
    endpoint: str,
    params: Optional[Dict[str, Any]] = None,
) -> requests.Response:
    url = f"{INDEED_API_BASE_URL}{endpoint}"
    headers = {
        "Authorization": f"Bearer {token}",
        "Accept": "application/json",
    }
    response = requests.get(url, headers=headers, params=params)
    try:
        response.raise_for_status()
    except requests.exceptions.HTTPError as e:
        raise RuntimeError(f"{e} - Response body: {response.text}") from e
    return response


def _paginate_campaigns(token: str) -> Iterator[Dict[str, Any]]:
    cursor: Optional[str] = None
    while True:
        params: Dict[str, Any] = {"perPage": 500}
        if cursor:
            params["start"] = cursor
        response = _api_request(token, "/campaigns", params=params)
        data = response.json()

        campaigns = data.get("data", {}).get("Campaigns", [])
        for campaign in campaigns:
            yield campaign

        links = data.get("meta", {}).get("links", [])
        next_link = next((link for link in links if link.get("rel") == "next"), None)

        if not next_link:
            break

        href = next_link.get("href", "")
        if "start=" in href:
            cursor = href.split("start=")[1].split("&")[0]
        else:
            break


def _get_campaign_details(token: str, campaign_id: str) -> Dict[str, Any]:
    response = _api_request(token, f"/campaigns/{campaign_id}")
    return response.json().get("data", {})


def _get_campaign_budget(token: str, campaign_id: str) -> Optional[Dict[str, Any]]:
    try:
        response = _api_request(token, f"/campaigns/{campaign_id}/budget")
        data = response.json().get("data", {})
        data["campaignId"] = campaign_id
        return data
    except requests.exceptions.HTTPError as e:
        if e.response.status_code == 404:
            return None
        raise


def _get_campaign_jobs(token: str, campaign_id: str) -> Iterator[Dict[str, Any]]:
    try:
        response = _api_request(
            token,
            f"/campaigns/{campaign_id}/jobDetails",
        )
        data = response.json()

        jobs = data.get("data", {}).get("entries", [])
        for job in jobs:
            job["campaignId"] = campaign_id
            yield job
    except requests.exceptions.HTTPError as e:
        if e.response is not None and e.response.status_code == 404:
            return
        if e.response is not None and e.response.status_code == 429:
            time.sleep(5)
            yield from _get_campaign_jobs(token, campaign_id)
            return
        raise


def _get_campaign_properties(token: str, campaign_id: str) -> Optional[Dict[str, Any]]:
    try:
        response = _api_request(token, f"/campaigns/{campaign_id}/properties")
        data = response.json().get("data", {})
        data["campaignId"] = campaign_id
        return data
    except requests.exceptions.HTTPError as e:
        if e.response.status_code == 404:
            return None
        raise


def _get_campaign_stats(
    token: str, campaign_id: str, start_date: str, end_date: str
) -> Iterator[Dict[str, Any]]:
    try:
        response = _api_request(
            token,
            f"/campaigns/{campaign_id}/stats",
            params={"startDate": start_date, "endDate": end_date},
        )
        data = response.json().get("data", {})

        stats = data.get("Stats", []) or data.get("entries", [])
        for stat in stats:
            stat["campaignId"] = campaign_id
            yield stat
    except requests.exceptions.HTTPError as e:
        if e.response.status_code == 404:
            return
        raise


def _get_account(token: str) -> Dict[str, Any]:
    response = _api_request(token, "/account")
    return response.json().get("data", {})


def _get_traffic_report_for_day(
    token: str, date: str, max_retries: int = 10, retry_delay: int = 5
) -> Iterator[Dict[str, Any]]:
    try:
        start_dt = datetime.strptime(date, "%Y-%m-%d")
        end_dt = start_dt + timedelta(days=1)
        end_date_str = end_dt.strftime("%Y-%m-%d")

        response = _api_request(
            token,
            "/stats",
            params={"startDate": date, "endDate": end_date_str, "v": "8"},
        )

        if response.status_code == 202:
            location = response.json().get("data", {}).get("location", "")
            if not location:
                raise ValueError(f"API returned 202 but no location for date {date}")

            if location.startswith("/v1"):
                location = location[3:]

            for attempt in range(max_retries):
                time.sleep(retry_delay)
                report_response = _api_request(token, location)

                if report_response.status_code == 200:
                    content_type = report_response.headers.get("Content-Type", "")
                    if "text/csv" in content_type or "application/csv" in content_type:
                        for row in _parse_csv(report_response.text):
                            row["date"] = date
                            yield row
                        return
            return
    except requests.exceptions.HTTPError as e:
        body = e.response.text if e.response is not None else "N/A"
        raise RuntimeError(f"{e} - Response body: {body}") from e


def _get_traffic_report(
    token: str,
    start_date: str,
    end_date: str,
    max_retries: int = 10,
    retry_delay: int = 5,
) -> Iterator[Dict[str, Any]]:
    start = datetime.strptime(start_date, "%Y-%m-%d")
    end = datetime.strptime(end_date, "%Y-%m-%d") + timedelta(days=1)

    current = start
    while current < end:
        date_str = current.strftime("%Y-%m-%d")
        yield from _get_traffic_report_for_day(
            token, date_str, max_retries, retry_delay
        )
        current += timedelta(days=1)


def _parse_csv(csv_content: str) -> Iterator[Dict[str, Any]]:
    reader = csv.DictReader(io.StringIO(csv_content))
    for row in reader:
        yield dict(row)
