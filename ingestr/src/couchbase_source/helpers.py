"""Helper functions for Couchbase source."""

from datetime import datetime, timedelta
from typing import Any, Dict, Iterator, Optional

import dlt
from couchbase.auth import PasswordAuthenticator  # type: ignore[import-untyped]
from couchbase.cluster import Cluster  # type: ignore[import-untyped]
from couchbase.options import (  # type: ignore[import-untyped]
    ClusterOptions,
    QueryOptions,
)
from dlt.common.configuration import configspec
from dlt.common.time import ensure_pendulum_datetime


@configspec
class CouchbaseConfiguration:
    """Configuration for Couchbase source."""

    connection_string: str = dlt.secrets.value
    username: str = dlt.secrets.value
    password: str = dlt.secrets.value
    bucket: str = dlt.config.value
    scope: Optional[str] = dlt.config.value
    collection: Optional[str] = dlt.config.value


def client_from_credentials(
    connection_string: str, username: str, password: str
) -> Cluster:
    """
    Create a Couchbase cluster client from credentials.

    Args:
        connection_string: Couchbase connection string
            - Local/self-hosted: 'couchbase://localhost'
            - Capella (cloud): 'couchbases://your-instance.cloud.couchbase.com'
        username: Couchbase username
        password: Couchbase password

    Returns:
        Cluster: Connected Couchbase cluster instance
    """
    auth = PasswordAuthenticator(username, password)
    options = ClusterOptions(auth)

    # Apply wan_development profile for Capella (couchbases://) connections
    # This helps avoid latency issues when accessing from different networks
    if connection_string.startswith("couchbases://"):
        options.apply_profile("wan_development")

    cluster = Cluster(connection_string, options)
    cluster.wait_until_ready(timedelta(seconds=30))

    return cluster


def fetch_documents(
    cluster: Cluster,
    bucket_name: str,
    scope_name: str,
    collection_name: str,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    limit: Optional[int] = None,
    chunk_size: Optional[int] = 1000,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch documents from a Couchbase collection using N1QL queries.

    Args:
        cluster: Couchbase cluster instance
        bucket_name: Name of the bucket
        scope_name: Name of the scope
        collection_name: Name of the collection
        incremental: Incremental loading configuration
        limit: Maximum number of documents to fetch
        chunk_size: Number of documents to fetch per batch

    Yields:
        Dict[str, Any]: Document data
    """
    # Build N1QL query with full path
    full_collection_path = f"`{bucket_name}`.`{scope_name}`.`{collection_name}`"
    n1ql_query = f"SELECT META().id as id, c.* FROM {full_collection_path} c"

    # Add incremental filter if provided
    if incremental and incremental.cursor_path:
        where_clause = f" WHERE {incremental.cursor_path} >= $start_value"
        if incremental.end_value is not None:
            where_clause += f" AND {incremental.cursor_path} < $end_value"
        n1ql_query += where_clause

    # Add limit if provided
    if limit:
        n1ql_query += f" LIMIT {limit}"

    # Execute query
    try:
        query_options = QueryOptions()

        # Add parameters if incremental
        if incremental and incremental.cursor_path:
            named_parameters = {"start_value": incremental.last_value}
            if incremental.end_value is not None:
                named_parameters["end_value"] = incremental.end_value
            query_options = QueryOptions(named_parameters=named_parameters)

        result = cluster.query(n1ql_query, query_options)

        # Yield documents
        count = 0
        for row in result:
            doc = dict(row)

            # Convert datetime fields to proper format
            if (
                incremental
                and incremental.cursor_path
                and incremental.cursor_path in doc
            ):
                cursor_value = doc[incremental.cursor_path]
                if isinstance(cursor_value, (str, datetime)):
                    doc[incremental.cursor_path] = ensure_pendulum_datetime(
                        cursor_value
                    )

            yield doc

            count += 1
            if limit and count >= limit:
                break

    except Exception as e:
        raise Exception(f"Error executing Couchbase N1QL query: {str(e)}")
