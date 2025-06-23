# Internet Society Pulse source using dlt's REST API utilities
from __future__ import annotations

from typing import Any, Dict, Iterable, List, Optional

import dlt
from dlt.sources.rest_api import EndpointResource, RESTAPIConfig, rest_api_resources

GLOBAL_METRICS: Dict[str, str] = {
    "dnssec_adoption": "dnssec/adoption",
    "dnssec_validation": "dnssec/validation",
    "http": "http",
    "http3": "http3",
    "https": "https",
    "ipv6": "ipv6",
    "net_loss": "net-loss",
    "resilience": "resilience",
    "roa": "roa",
    "rov": "rov",
    "tls": "tls",
    "tls13": "tls13",
}


@dlt.source
def pulse_source(
    token: str,
    start_date: str,
    end_date: Optional[str] = None,
    metrics: Optional[Iterable[str]] = None,
    topsites: Optional[bool] = None,
    ip_version: Optional[str] = None,
) -> Iterable[dlt.sources.DltResource]:
    """Create resources for Internet Society Pulse metrics.

    Args:
        token: Bearer token for the API.
        start_date: First date of the data range (YYYY-MM-DD).
        end_date: Last date of the data range.
        metrics: Subset of metrics to fetch. Defaults to all available metrics.
        topsites: Optional flag used by some endpoints.
        ip_version: IP version parameter used by some endpoints.
    """
    if metrics is None:
        metrics = GLOBAL_METRICS.keys()

    headers = {"Authorization": f"Bearer {token}"}

    resources: List[EndpointResource] = []
    for name in metrics:
        path = GLOBAL_METRICS.get(name)
        if not path:
            continue

        endpoint: Dict[str, Any] = {
            "path": path,
            "params": {
                "start_date": "{incremental.start_value}",
            },
            "incremental": {
                "cursor_path": "date",
                "start_param": "start_date",
                "end_param": "end_date",
                "initial_value": start_date,
                "end_value": end_date,
                "range_start": "closed",
                "range_end": "closed",
            },
            "paginator": "single_page",
        }

        if end_date is not None:
            endpoint["params"]["end_date"] = end_date
        if topsites is not None and name in {"http", "https"}:
            endpoint["params"]["topsites"] = topsites
        if ip_version is not None and name in {"roa", "rov", "tls", "tls13"}:
            endpoint["params"]["ip_version"] = ip_version

        resources.append({
            "name": name,
            "write_disposition": "merge",
            "primary_key": "date",
            "columns": {"date": {"data_type": "date"}},
            "endpoint": endpoint,
        })

    config: RESTAPIConfig = {
        "client": {
            "base_url": "https://pulse.internetsociety.org/api/",
            "headers": headers,
        },
        "resource_defaults": {
            "write_disposition": "merge",
            "primary_key": "date",
        },
        "resources": resources,
    }

    yield from rest_api_resources(config)
