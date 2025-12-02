"""Bruin source for fetching pipeline and asset data from Bruin Cloud API"""

from typing import Iterator

import dlt
from dlt.sources.helpers import requests

BASE_URL = "https://cloud.getbruin.com/api/v1"


def _fetch_pipelines(headers: dict) -> list:
    """Fetch pipelines data from API."""
    response = requests.get(f"{BASE_URL}/pipelines", headers=headers)
    response.raise_for_status()
    return response.json()


@dlt.source(name="bruin", max_table_nesting=0)
def bruin_source(api_token: str):
    """
    A dlt source for the Bruin Cloud API.

    Args:
        api_token (str): The API token for authentication.

    Returns:
        DltResource: Resources for pipelines and assets data.
    """
    headers = {"Authorization": f"Bearer {api_token}"}

    @dlt.resource(write_disposition="replace")
    def pipelines() -> Iterator[dict]:
        """
        Fetches all pipelines and yields pipeline_id and pipeline_name for each.
        """
        data = _fetch_pipelines(headers)

        for pipeline in data:
            yield {
                "name": pipeline.get("name"),
                "description": pipeline.get("description") or "",
                "project": pipeline.get("project"),
                "owner": pipeline.get("owner") or "",
                "default_connections": pipeline.get("default_connections"),
                "schedule": pipeline.get("schedule"),
                "commit": pipeline.get("commit"),
                "start_date": pipeline.get("start_date"),
                "variables": pipeline.get("variables") or [],
            }

    @dlt.resource(write_disposition="replace")
    def assets() -> Iterator[dict]:
        """
        Fetches all assets from all pipelines (same endpoint as pipelines).
        """
        data = _fetch_pipelines(headers)

        for pipeline in data:
            pipeline_assets = pipeline.get("assets", [])
            for asset in pipeline_assets:
                yield {
                    "name": asset.get("name"),
                    "type": asset.get("type"),
                    "pipeline": asset.get("pipeline"),
                    "project": asset.get("project"),
                    "uri": asset.get("uri"),
                    "description": asset.get("description"),
                    "upstreams": asset.get("upstreams") or [],
                    "downstream": asset.get("downstream") or [],
                    "owner": asset.get("owner") or None,
                    "content": asset.get("content"),
                    "columns": asset.get("columns") or [],
                    "materialization": asset.get("materialization") or None,
                    "parameters": asset.get("parameters") or None,
                    "secrets": asset.get("secrets") or [],
                    "tags": asset.get("tags") or [],
                    "meta": asset.get("meta") or None,
                    "domains": asset.get("domains") or [],
                    "connection": asset.get("connection") or None,
                    "image": asset.get("image") or None,
                    "extends": asset.get("extends") or [],
                    "metadata": asset.get("metadata") or [],
                    "snowflake": asset.get("snowflake") or None,
                    "athena": asset.get("athena") or None,
                    "interval": asset.get("interval") or None,
                }

    return pipelines, assets
