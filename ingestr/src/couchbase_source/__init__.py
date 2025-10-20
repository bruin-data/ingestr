"""Source that loads data from Couchbase Server.

This source provides data extraction from Couchbase Server using the REST API.
"""

from typing import Iterable

import dlt
from dlt.sources import DltResource

from .helpers import get_couchbase_data, get_capella_cloud_data


@dlt.source(max_table_nesting=0)
def couchbase_source(
    host: str = dlt.config.value,
    username: str = None,
    password: str = None,
    port: int = 8091,
    table_name: str = "pools_default",
    bucket_name: str = None,
    use_ssl: bool = False,
    organization_id: str = None,
    api_token: str = None,
) -> Iterable[DltResource]:
    """
    Loads data from Couchbase Server or Capella (Cloud).

    Args:
        host (str): The Couchbase server hostname or IP address (e.g., 'localhost' or 'cloudapi.cloud.couchbase.com')
        username (str): The username for authentication (e.g., 'Administrator')
        password (str): The password for authentication (API token for Capella Cloud)
        port (int): The REST API port (default: 8091 for server, 443 for Capella Cloud API)
        table_name (str): The endpoint to fetch
        bucket_name (str): Optional bucket name for bucket-specific endpoints (e.g., scopes)
        use_ssl (bool): Use HTTPS for Capella/Cloud (default: False)
        organization_id (str): Organization ID for Capella Cloud API endpoints (required for cloud_users)

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
        "nodes_self": ("nodes/self", "nodes_self"),
        "autoCompaction": ("settings/autoCompaction", "autoCompaction"),
        "serverGroups": ("pools/default/serverGroups", "serverGroups"),
        "remoteClusters": ("pools/default/remoteClusters", "remoteClusters"),
        "replications": ("settings/replications/", "replications"),
        "audit": ("settings/audit", "audit"),
        "audit_descriptors": ("settings/audit/descriptors", "audit_descriptors"),
        "security": ("settings/security", "security"),
        "secretsManagement": ("nodes/self/secretsManagement", "secretsManagement"),
        "query_service": ("query/service", "query_service"),
        "admin_clusters": ("admin/clusters", "admin_clusters"),
        "admin_active_requests": ("admin/active_requests", "admin_active_requests"),
        "admin_stats": ("admin/stats", "admin_stats"),
        "admin_settings": ("admin/settings", "admin_settings"),
        "querySettings": ("settings/querySettings", "querySettings"),
        "cluster_plan": ("api/v1/cluster/plan", "cluster_plan"),
        "api_query": ("api/query", "api_query"),
        "managerOptions": ("api/managerOptions", "managerOptions"),
        "analytics_service": ("analytics/service", "analytics_service"),
    }

    # Handle capella_users endpoint - Capella Cloud API
    # Format: capella_users/{organization_id}
    if table_name.startswith("capella_users/"):
        if not api_token:
            raise ValueError("api_token is required for 'capella_users' endpoint")

        # Extract organization_id from table_name
        org_id = table_name.split("/", 1)[1]
        endpoint_path = f"v4/organizations/{org_id}/users"
        resource_name = "capella_users"

        yield dlt.resource(
            get_capella_cloud_data(api_token, endpoint_path),
            name=resource_name,
            write_disposition="replace",
        )
    # Handle scopes endpoint - get scopes for all buckets
    elif table_name == "scopes":
        # First, get all buckets
        buckets_data = list(
            get_couchbase_data(
                host, port, username, password, "pools/default/buckets", use_ssl
            )
        )

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
                    get_couchbase_data(
                        host, port, username, password, endpoint_path, use_ssl
                    ),
                    name=resource_name,
                    write_disposition="replace",
                )
    elif table_name in endpoint_map:
        endpoint_path, resource_name = endpoint_map[table_name]

        yield dlt.resource(
            get_couchbase_data(host, port, username, password, endpoint_path, use_ssl),
            name=resource_name,
            write_disposition="replace",
        )
    else:
        supported = ", ".join(list(endpoint_map.keys()) + ["scopes", "capella_users"])
        raise ValueError(f"Unsupported table: {table_name}. Supported tables: {supported}")
