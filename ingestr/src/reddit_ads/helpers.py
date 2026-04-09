import time

import requests
from dlt.sources.helpers.requests import Client
from pendulum import Date

MONETARY_FIELDS = {"spend", "ecpm", "cpc"}

BASE_URL = "https://ads-api.reddit.com/api/v3"

LEVEL_ID_FIELDS = {
    "ACCOUNT": "account_id",
    "CAMPAIGN": "campaign_id",
    "AD_GROUP": "ad_group_id",
    "AD": "ad_id",
}

VALID_LEVELS = {"ACCOUNT", "CAMPAIGN", "AD_GROUP", "AD"}

VALID_BREAKDOWNS = {
    "date",
    "country",
    "region",
    "community",
    "placement",
    "device_os",
    "gender",
    "interest",
    "keyword",
    "carousel_card",
}


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


def handle_rate_limit(response: requests.Response) -> None:
    remaining = response.headers.get("X-RateLimit-Remaining")
    reset = response.headers.get("X-RateLimit-Reset")
    if remaining is not None and reset is not None:
        try:
            if float(remaining) < 2:
                sleep_time = float(reset)
                if sleep_time > 0:
                    time.sleep(sleep_time)
        except (ValueError, TypeError):
            pass


def convert_microcurrency(records: list[dict], metrics: list[str]) -> list[dict]:
    monetary = MONETARY_FIELDS & {m.lower() for m in metrics}
    if not monetary:
        return records
    for record in records:
        for field in monetary:
            if field in record and record[field] is not None:
                record[field] = record[field] / 1_000_000
    return records


def parse_custom_table(table: str) -> tuple[str, list[str], list[str]]:
    parts = table.split(":")
    if len(parts) != 3:
        raise ValueError(
            "Invalid custom table format. Expected: custom:<level>,<breakdowns>:<metrics>"
        )

    dimensions = [d.strip() for d in parts[1].split(",") if d.strip()]
    if not dimensions:
        raise ValueError("At least a level is required in the dimensions segment")

    level = dimensions[0].upper()
    if level not in VALID_LEVELS:
        raise ValueError(
            f"Invalid level '{level}'. Must be one of: {', '.join(sorted(VALID_LEVELS))}"
        )

    breakdowns = dimensions[1:]
    if len(breakdowns) > 2:
        raise ValueError("Reddit Ads supports at most 2 breakdowns per report")

    for b in breakdowns:
        if b not in VALID_BREAKDOWNS:
            raise ValueError(
                f"Invalid breakdown '{b}'. Must be one of: {', '.join(sorted(VALID_BREAKDOWNS))}"
            )

    metrics = [m.strip().upper() for m in parts[2].split(",") if m.strip()]
    if not metrics:
        raise ValueError("At least one metric is required")

    return level, breakdowns, metrics


class RedditAdsAPI:
    def __init__(self, access_token: str):
        self.headers = {
            "Authorization": f"Bearer {access_token}",
            "User-Agent": "ingestr/1.0",
        }
        self.client = create_client()

    def fetch_pages(self, url: str, page_size: int = 100):
        separator = "&" if "?" in url else "?"
        paginated_url = f"{url}{separator}page_size={page_size}"

        while True:
            response = self.client.get(url=paginated_url, headers=self.headers)

            if response.status_code != 200:
                raise ValueError(
                    f"Reddit Ads API Error ({response.status_code}): {response.text}"
                )

            handle_rate_limit(response)

            result = response.json()
            elements = result.get("data", [])

            if not elements:
                break

            yield elements

            pagination = result.get("pagination", {})
            next_url = pagination.get("next_url")
            if not next_url:
                break

            paginated_url = next_url


class RedditAdsReportAPI:
    def __init__(
        self,
        access_token: str,
        account_ids: list[str],
        level: str,
        breakdowns: list[str],
        metrics: list[str],
    ):
        self.headers = {
            "Authorization": f"Bearer {access_token}",
            "User-Agent": "ingestr/1.0",
            "Content-Type": "application/json",
        }
        self.account_ids = account_ids
        self.level = level
        self.breakdowns = breakdowns
        self.metrics = metrics
        self.client = create_client()

    def fetch_report(self, start_date: Date, end_date: Date):
        body = {
            "start_date": start_date.to_date_string(),
            "end_date": end_date.to_date_string(),
            "level": self.level,
            "metrics": self.metrics,
            "breakdowns": self.breakdowns,
        }

        for account_id in self.account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/reports"
            response = self.client.post(url=url, json=body, headers=self.headers)

            if response.status_code != 200:
                raise ValueError(
                    f"Reddit Ads Report API Error ({response.status_code}): {response.text}"
                )

            handle_rate_limit(response)

            result = response.json()
            records = result.get("data", [])

            if not records:
                continue

            for record in records:
                record["account_id"] = account_id

            records = convert_microcurrency(records, self.metrics)
            yield records
