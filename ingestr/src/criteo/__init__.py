import json
from typing import Iterable, Sequence

import dlt
from dlt.sources import DltResource
from dlt.sources.helpers import requests


@dlt.source(max_table_nesting=0)
def criteo_source(
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
    advertiser_ids: Sequence[str] = dlt.config.value,
    dimensions: Sequence[str] = dlt.config.value,
    metrics: Sequence[str] = dlt.config.value,
    start_date: str = dlt.config.value,
    end_date: str = dlt.config.value,
) -> DltResource:
    """Load reporting data from the Criteo Marketing API."""

    token_resp = requests.post(
        "https://api.criteo.com/oauth2/token",
        data={
            "grant_type": "client_credentials",
            "client_id": client_id,
            "client_secret": client_secret,
        },
        timeout=60,
    )
    token_resp.raise_for_status()
    access_token = token_resp.json().get("access_token")

    headers = {"Authorization": f"Bearer {access_token}"}
    report_body = {
        "advertiserIds": ",".join(advertiser_ids),
        "startDate": start_date,
        "endDate": end_date,
        "dimensions": list(dimensions),
        "metrics": list(metrics),
        "format": "json",
    }

    @dlt.resource(write_disposition="merge", primary_key=list(dimensions))
    def campaign_report() -> Iterable[dict]:
        response = requests.post(
            "https://api.criteo.com/v1/statistics/report",
            headers=headers,
            json=report_body,
            timeout=60,
        )
        response.raise_for_status()
        data = response.json()
        if isinstance(data, str):
            data = json.loads(data)
        for item in data.get("data", []):
            yield item

    return campaign_report
