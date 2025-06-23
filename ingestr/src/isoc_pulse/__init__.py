from typing import Any, Dict, Iterable, List, Optional

import dlt
from dataclasses import dataclass
from dlt.sources.rest_api import EndpointResource, RESTAPIConfig, rest_api_resources

METRICS: Dict[str, str] = {
    "dnssec_adoption": "dnssec/adoption",
    "dnssec_tld_adoption": "dnssec/adoption",
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
    metric: str,
    opts: List[str],
    end_date: Optional[str] = None,
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

    headers = {"Authorization": f"Bearer {token}"}

    resources: List[EndpointResource] = []

    cfg = get_metric_cfg(metric, opts)
    endpoint: Dict[str, Any] = {
        "path": cfg.path,
        "params": {
            "start_date": "{incremental.start_value}",
            **cfg.params,
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
    
    resources.append({
        "name": metric,
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

@dataclass
class MetricCfg:
    path: str
    params: Dict[str, Any]

def get_metric_cfg(metric: str, opts: List[str]) -> MetricCfg:
    path = METRICS.get(metric)
    if len(opts) == 0:
        return MetricCfg(path=path, params={})
    
    if metric == "https":
        return MetricCfg(
            path=f"{path}/country/{opts[-1]}",
            params={
                "topsites": True if "topsites" in opts else False,
            }
        )
    elif metric in ["dnssec_validation", "dnssec_tld_adoption"]:
        return MetricCfg(
            path=f"{path}/country/{opts[-1]}",
            params={}
        )
    elif metric == "dnssec_adoption":
        return MetricCfg(
            path=f"{path}/domains/{opts[-1]}",
            params={}
        )
    elif metric == "ipv6":
        if "topsites" in opts:
            return MetricCfg(
                path=path,
                params={"topsites": True}
            )
        else:
            return MetricCfg(
                path=f"{path}/country/{opts[-1]}",
                params={}
            )

    