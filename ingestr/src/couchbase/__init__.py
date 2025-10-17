"""Source that loads collections from a Couchbase database, supports incremental loads."""

from typing import Any, Dict, Iterable, List, Optional

import dlt
from dlt.sources import DltResource

from .helpers import (
    CouchbaseCollectionConfiguration,
    CouchbaseCollectionResourceConfiguration,
    cluster_from_credentials,
    collection_documents,
    parse_couchbase_uri,
)


@dlt.source(max_table_nesting=0)
def couchbase(
    connection_string: str = dlt.config.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
    bucket: str = dlt.config.value,
    scope: Optional[str] = None,
    collection_names: Optional[List[str]] = None,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    limit: Optional[int] = None,
    filter_: Optional[Dict[str, Any]] = None,
    projection: Optional[List[str]] = None,
) -> Iterable[DltResource]:
    """
    A DLT source which loads data from a Couchbase database.
    Resources are automatically created for each collection in the scope or from the given list.

    Args:
        connection_string (str): Couchbase cluster connection string (e.g., "couchbase://localhost").
        username (str): Couchbase username.
        password (str): Couchbase password.
        bucket (str): Bucket name.
        scope (Optional[str]): Scope name (defaults to "_default").
        collection_names (Optional[List[str]]): The list of collections to load.
        incremental (Optional[dlt.sources.incremental]): Option to enable incremental loading.
            E.g., `incremental=dlt.sources.incremental('updated_at', pendulum.parse('2022-01-01T00:00:00Z'))`
        write_disposition (str): Write disposition of the resource.
        limit (Optional[int]): The maximum number of documents to load per collection.
        filter_ (Optional[Dict[str, Any]]): The filter to apply to the collection.
        projection (Optional[List[str]]): The fields to select from documents.

    Returns:
        Iterable[DltResource]: A list of DLT resources for each collection to be loaded.
    """
    # Set up Couchbase cluster
    cluster = cluster_from_credentials(connection_string, username, password)

    # Use default scope if not specified
    scope_name = scope or "_default"

    # Get bucket reference
    bucket_obj = cluster.bucket(bucket)
    scope_obj = bucket_obj.scope(scope_name)

    # Get collection names
    if not collection_names:
        # If no collections specified, we need to query the collections
        # Note: Couchbase doesn't have a direct API to list collections,
        # so we'll need to use N1QL query
        try:
            query = f"SELECT RAW name FROM system:keyspaces WHERE bucket = '{bucket}' AND scope = '{scope_name}'"
            result = cluster.query(query)
            collection_names = [row for row in result]
        except Exception:
            # Fallback to default collection if query fails
            collection_names = ["_default"]

    for collection_name in collection_names:
        yield dlt.resource(  # type: ignore
            collection_documents,
            name=collection_name,
            primary_key="_id",
            write_disposition=write_disposition,
            spec=CouchbaseCollectionConfiguration,
            max_table_nesting=0,
        )(
            cluster,
            bucket,
            scope_name,
            collection_name,
            incremental=incremental,
            limit=limit,
            filter_=filter_ or {},
            projection=projection,
        )


@dlt.resource(
    name=lambda args: args["collection"],
    standalone=True,
    spec=CouchbaseCollectionResourceConfiguration,
)
def couchbase_collection(
    connection_string: str = dlt.config.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
    bucket: str = dlt.config.value,
    scope: Optional[str] = None,
    collection: str = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    limit: Optional[int] = None,
    chunk_size: Optional[int] = 10000,
    filter_: Optional[Dict[str, Any]] = None,
    projection: Optional[List[str]] = dlt.config.value,
    custom_query: Optional[str] = None,
    kv_mode: bool = False,
    document_keys: Optional[List[str]] = None,
) -> DltResource:
    """
    A DLT source which loads a collection from a Couchbase database.

    Args:
        connection_string (str): Couchbase cluster connection string.
        username (str): Couchbase username.
        password (str): Couchbase password.
        bucket (str): Bucket name.
        scope (Optional[str]): Scope name (defaults to "_default").
        collection (str): The collection name to load.
        incremental (Optional[dlt.sources.incremental]): Option to enable incremental loading.
        write_disposition (str): Write disposition of the resource.
        limit (Optional[int]): The number of documents to load.
        chunk_size (Optional[int]): The number of documents to load in each batch.
        filter_ (Optional[Dict[str, Any]]): The filter to apply to the collection.
        projection (Optional[List[str]]): The fields to select from documents.
        custom_query (Optional[str]): Custom N1QL query to execute instead of building one.
        kv_mode (bool): Use Key-Value operations instead of N1QL queries.
        document_keys (Optional[List[str]]): List of document keys to load when using KV mode.

    Returns:
        DltResource: A DLT resource for the collection to be loaded.
    """
    # Set up Couchbase cluster
    cluster = cluster_from_credentials(connection_string, username, password)

    # Use default scope if not specified
    scope_name = scope or "_default"

    return dlt.resource(  # type: ignore
        collection_documents,
        name=collection,
        primary_key="_id",
        write_disposition=write_disposition,
    )(
        cluster,
        bucket,
        scope_name,
        collection,
        incremental=incremental,
        limit=limit,
        chunk_size=chunk_size,
        filter_=filter_ or {},
        projection=projection,
        custom_query=custom_query,
        kv_mode=kv_mode,
        document_keys=document_keys,
    )
