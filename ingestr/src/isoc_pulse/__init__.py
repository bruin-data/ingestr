import math
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Dict, Iterable, List, Optional

import dlt
from dlt.sources.rest_api import RESTAPIConfig, rest_api_resources

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
    validate(metric, opts)
    cfg = get_metric_cfg(metric, opts, start_date)
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

    resources = [
        {
            "name": metric,
            "write_disposition": "merge",
            "primary_key": "date",
            "columns": {"date": {"data_type": "date"}},
            "endpoint": endpoint,
        }
    ]

    config: RESTAPIConfig = {
        "client": {
            "base_url": "https://pulse.internetsociety.org/api/",
            "headers": {"Authorization": f"Bearer {token}"},
        },
        "resource_defaults": {
            "write_disposition": "merge",
            "primary_key": "date",
        },
        "resources": resources,  # type:ignore
    }
    res = rest_api_resources(config)
    if metric == "net_loss":
        res[0].add_map(add_date(start_date))
    yield from res


@dataclass
class MetricCfg:
    path: str
    params: Dict[str, Any]


def get_metric_cfg(metric: str, opts: List[str], start_date: str) -> MetricCfg:
    path = METRICS.get(metric)
    if path is None:
        raise ValueError(f"Unknown metric '{metric}'.")
    if len(opts) == 0:
        return MetricCfg(path=path, params={})

    if metric == "https":
        return MetricCfg(
            path=f"{path}/country/{opts[-1]}",
            params={
                "topsites": True if "topsites" in opts else False,
            },
        )
    elif metric in ["dnssec_validation", "dnssec_tld_adoption"]:
        return MetricCfg(path=f"{path}/country/{opts[-1]}", params={})
    elif metric == "dnssec_adoption":
        return MetricCfg(path=f"{path}/domains/{opts[-1]}", params={})
    elif metric == "ipv6":
        if "topsites" in opts:
            return MetricCfg(path=path, params={"topsites": True})
        return MetricCfg(path=f"{path}/country/{opts[-1]}", params={})
    elif metric == "roa":
        if len(opts) > 1:
            return MetricCfg(
                path=f"{path}/country/{opts[-1]}", params={"ip_version": opts[-2]}
            )
        return MetricCfg(path=path, params={"ip_version": opts[-1]})
    elif metric == "net_loss":
        return MetricCfg(
            path=path,
            params={
                "country": opts[-1],
                "shutdown_type": opts[-2],
            },
        )
    elif metric == "resilience":
        date = datetime.strptime(start_date, "%Y-%m-%d")
        return MetricCfg(
            path=path,
            params={
                "country": opts[-1],
                "year": date.year,
                "quarter": math.floor(date.month / 4) + 1,
            },
        )
    else:
        raise ValueError(
            f"Unsupported metric '{metric}' with options {opts}. "
            "Please check the metric and options."
        )


def add_date(start_date: str):
    def transform(item: dict):
        item["date"] = start_date
        return item

    return transform


def validate(metric: str, opts: List[str]) -> None:
    nopts = len(opts)
    if metric == "net_loss" and nopts != 2:
        raise ValueError(
            "For 'net_loss' metric, two options are required: "
            "'shutdown_type' and 'country'."
        )
    if nopts > 0 and metric in ["http", "http3", "tls", "tls13", "rov"]:
        raise ValueError(f"metric '{metric}' does not support options. ")
