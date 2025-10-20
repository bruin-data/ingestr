"""Source that loads data from Couchbase buckets, supports incremental loads."""

from typing import Optional

import dlt
from dlt.sources import DltResource

from .helpers import (
    CouchbaseConfiguration,
    client_from_credentials,
    fetch_documents,
)


@dlt.source(max_table_nesting=0)
def couchbase_source(
    connection_string: str = dlt.secrets.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
    bucket: str = dlt.config.value,
    scope: Optional[str] = dlt.config.value,
    collection: Optional[str] = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    limit: Optional[int] = None,
) -> DltResource:
    """
    A DLT source which loads data from a Couchbase bucket using Couchbase Python SDK.

    Args:
        connection_string (str): Couchbase connection string (e.g., 'couchbase://localhost')
        username (str): Couchbase username
        password (str): Couchbase password
        bucket (str): Bucket name to load data from
        scope (Optional[str]): Scope name (defaults to '_default')
        collection (Optional[str]): Collection name (defaults to '_default')
        incremental (Optional[dlt.sources.incremental]): Option to enable incremental loading.
            E.g., `incremental=dlt.sources.incremental('updated_at', pendulum.parse('2022-01-01T00:00:00Z'))`
        write_disposition (str): Write disposition of the resource.
        limit (Optional[int]): The maximum number of documents to load.

    Returns:
        DltResource: A DLT resource for the Couchbase collection.
    """
    # Set up Couchbase client
    cluster = client_from_credentials(connection_string, username, password)

    resource_name = f"{bucket}_{scope}_{collection}"

    return dlt.resource(  # type: ignore[call-overload, arg-type]
        fetch_documents,
        name=resource_name,
        primary_key="id",
        write_disposition=write_disposition or "replace",
        spec=CouchbaseConfiguration,
        max_table_nesting=0,
    )(
        cluster=cluster,
        bucket_name=bucket,
        scope_name=scope,
        collection_name=collection,
        incremental=incremental,
        limit=limit,
    )


@dlt.resource(
    name=lambda args: f"{args['bucket']}_{args['scope']}_{args['collection']}",
    standalone=True,
    spec=CouchbaseConfiguration,  # type: ignore[arg-type]
)
def couchbase_collection(
    connection_string: str = dlt.secrets.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
    bucket: str = dlt.config.value,
    scope: Optional[str] = dlt.config.value,
    collection: Optional[str] = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    limit: Optional[int] = None,
    chunk_size: Optional[int] = 1000,
) -> DltResource:
    """
    A DLT resource which loads a collection from Couchbase.

    Args:
        connection_string (str): Couchbase connection string (e.g., 'couchbase://localhost')
        username (str): Couchbase username
        password (str): Couchbase password
        bucket (str): Bucket name to load data from
        scope (Optional[str]): Scope name (defaults to '_default')
        collection (Optional[str]): Collection name (defaults to '_default')
        incremental (Optional[dlt.sources.incremental]): Option to enable incremental loading.
        write_disposition (str): Write disposition of the resource.
        limit (Optional[int]): The maximum number of documents to load.
        chunk_size (Optional[int]): The number of documents to load in each batch.

    Returns:
        DltResource: A DLT resource for the Couchbase collection.
    """
    # Set up Couchbase client
    cluster = client_from_credentials(connection_string, username, password)

    return dlt.resource(  # type: ignore[call-overload]
        fetch_documents,
        name=f"{bucket}_{scope}_{collection}",
        primary_key="id",
        write_disposition=write_disposition or "replace",
    )(
        cluster=cluster,
        bucket_name=bucket,
        scope_name=scope,
        collection_name=collection,
        incremental=incremental,
        limit=limit,
        chunk_size=chunk_size,
    )
