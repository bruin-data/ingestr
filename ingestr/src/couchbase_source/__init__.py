"""Source that loads data from Couchbase Server.

This source provides data extraction from Couchbase Server using the REST API.
"""

from typing import Any, Dict, Iterable

import dlt
import requests
from dlt.sources import DltResource
from requests.auth import HTTPBasicAuth


@dlt.source(max_table_nesting=0)
def couchbase_source(
    host: str = dlt.config.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
    port: int = 8091,
    table_name: str = "pools_default",
    bucket_name: str = None,
) -> Iterable[DltResource]:
    """
    Loads data from Couchbase Server.

    Args:
        host (str): The Couchbase server hostname or IP address (e.g., 'localhost')
        username (str): The username for authentication (e.g., 'Administrator')
        password (str): The password for authentication
        port (int): The REST API port (default: 8091)
        table_name (str): The endpoint to fetch
        bucket_name (str): Optional bucket name for bucket-specific endpoints (e.g., scopes)

    Yields:
        DltResource: Resource containing Couchbase data
    """
    # Map of endpoint names to their fetch functions
    endpoint_map = {
        "pools_default": ("pools/default", "pools_default"),
        "rebalanceProgress": ("pools/default/rebalanceProgress", "rebalanceProgress"),
        "retryRebalance": ("settings/retryRebalance", "retryRebalance"),
        "rebalance": ("settings/rebalance", "rebalance"),
        "autoFailover": ("settings/autoFailover", "autoFailover"),
        "maxParallelIndexers": ("settings/maxParallelIndexers", "maxParallelIndexers"),
        "memcachedGlobal": ("pools/default/settings/memcached/global", "memcachedGlobal"),
        "alerts": ("settings/alerts", "alerts"),
        "tasks": ("pools/default/tasks", "tasks"),
        "events": ("events", "events"),
        "eventsStreaming": ("eventsStreaming", "eventsStreaming"),
        "terseClusterInfo": ("pools/default/terseClusterInfo", "terseClusterInfo"),
        "nodes": ("pools/nodes", "nodes"),
        "nodeServices": ("pools/default/nodeServices", "nodeServices"),
        "prometheus_sd_config": ("prometheus_sd_config", "prometheus_sd_config"),
        "diag": ("diag", "diag"),
        "sasl_logs": ("sasl_logs", "sasl_logs"),
        "buckets": ("pools/default/buckets", "buckets"),
        "buckets_default": ("pools/default/buckets/default", "buckets_default"),
        "sampleBuckets": ("sampleBuckets", "sampleBuckets"),
    }

    # Handle scopes endpoint - get scopes for all buckets
    if table_name == "scopes":
        # First, get all buckets
        buckets_data = list(get_couchbase_data(host, port, username, password, "pools/default/buckets"))

        if buckets_data and isinstance(buckets_data[0], list):
            # If it's a list of buckets
            buckets_list = buckets_data[0]
        else:
            buckets_list = buckets_data

        # For each bucket, yield a resource with its scopes
        for bucket in buckets_list:
            if isinstance(bucket, dict) and "name" in bucket:
                bucket_name = bucket["name"]
                endpoint_path = f"pools/default/buckets/{bucket_name}/scopes"
                resource_name = f"scopes_{bucket_name}"

                yield dlt.resource(
                    get_couchbase_data(host, port, username, password, endpoint_path),
                    name=resource_name,
                    write_disposition="replace",
                )
    elif table_name in endpoint_map:
        endpoint_path, resource_name = endpoint_map[table_name]

        yield dlt.resource(
            get_couchbase_data(host, port, username, password, endpoint_path),
            name=resource_name,
            write_disposition="replace",
        )
    else:
        supported = ", ".join(list(endpoint_map.keys()) + ["scopes"])
        raise ValueError(f"Unsupported table: {table_name}. Supported tables: {supported}")


def get_couchbase_data(
    host: str, port: int, username: str, password: str, endpoint: str
) -> Iterable[Dict[str, Any]]:
    """
    Gets data from a Couchbase Server REST API endpoint.

    Args:
        host (str): The Couchbase server hostname
        port (int): The REST API port
        username (str): Username for authentication
        password (str): Password for authentication
        endpoint (str): The API endpoint path (e.g., 'pools/default')

    Yields:
        Dict[str, Any]: Data from the endpoint
    """
    url = f"http://{host}:{port}/{endpoint}"
    response = requests.get(url, auth=HTTPBasicAuth(username, password))
    response.raise_for_status()
    data = response.json()

    # Yield the complete data
    yield data
