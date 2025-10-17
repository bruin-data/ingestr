"""Couchbase database source helpers and utilities"""

import re
from datetime import datetime, timedelta
from itertools import islice
from typing import (
    TYPE_CHECKING,
    Any,
    Dict,
    Iterable,
    Iterator,
    List,
    Mapping,
    Optional,
    Tuple,
    Union,
)

import dlt
from dlt.common import logger
from dlt.common.configuration.specs import BaseConfiguration, configspec
from dlt.common.data_writers import TDataItemFormat
from dlt.common.schema import TTableSchema
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.common.utils import map_nested_in_place
from pendulum import _datetime

if TYPE_CHECKING:
    from couchbase.cluster import Cluster
    from couchbase.collection import Collection
    TCluster = Cluster
    TCollection = Collection
else:
    TCluster = Any
    TCollection = Any


class CouchbaseLoader:
    """Base loader for Couchbase collections"""

    def __init__(
        self,
        cluster: TCluster,
        bucket_name: str,
        scope_name: str,
        collection_name: str,
        chunk_size: int,
        incremental: Optional[dlt.sources.incremental[Any]] = None,
    ) -> None:
        self.cluster = cluster
        self.bucket_name = bucket_name
        self.scope_name = scope_name
        self.collection_name = collection_name
        self.incremental = incremental
        self.chunk_size = chunk_size

        # Get bucket, scope, and collection references
        self.bucket = cluster.bucket(bucket_name)
        self.scope = self.bucket.scope(scope_name)
        self.collection = self.scope.collection(collection_name)

        if incremental:
            self.cursor_field = incremental.cursor_path
            self.last_value = incremental.last_value
        else:
            self.cursor_field = None
            self.last_value = None

    def _build_where_clause(self, filter_: Optional[Dict[str, Any]] = None) -> Tuple[str, Dict[str, Any]]:
        """Build WHERE clause and parameters for N1QL query"""
        conditions = []
        params = {}

        # Add incremental filter if present
        if self.incremental and self.last_value:
            cursor_field = self.cursor_field
            if self.incremental.last_value_func is max:
                conditions.append(f"`{cursor_field}` >= ${cursor_field}_start")
                params[f"{cursor_field}_start"] = self.last_value
                if self.incremental.end_value:
                    conditions.append(f"`{cursor_field}` < ${cursor_field}_end")
                    params[f"{cursor_field}_end"] = self.incremental.end_value
            elif self.incremental.last_value_func is min:
                conditions.append(f"`{cursor_field}` <= ${cursor_field}_start")
                params[f"{cursor_field}_start"] = self.last_value
                if self.incremental.end_value:
                    conditions.append(f"`{cursor_field}` > ${cursor_field}_end")
                    params[f"{cursor_field}_end"] = self.incremental.end_value

        # Add custom filter if provided
        if filter_:
            for i, (key, value) in enumerate(filter_.items()):
                param_name = f"filter_{i}"
                conditions.append(f"`{key}` = ${param_name}")
                params[param_name] = value

        where_clause = " AND ".join(conditions) if conditions else ""
        return where_clause, params

    def _build_order_clause(self) -> str:
        """Build ORDER BY clause for incremental loading"""
        if not self.incremental or not self.last_value:
            return ""

        cursor_field = self.cursor_field
        if (
            self.incremental.row_order == "asc"
            and self.incremental.last_value_func is max
        ) or (
            self.incremental.row_order == "desc"
            and self.incremental.last_value_func is min
        ):
            return f"ORDER BY `{cursor_field}` ASC"
        elif (
            self.incremental.row_order == "asc"
            and self.incremental.last_value_func is min
        ) or (
            self.incremental.row_order == "desc"
            and self.incremental.last_value_func is max
        ):
            return f"ORDER BY `{cursor_field}` DESC"

        return ""

    def _execute_query(
        self,
        query: str,
        params: Optional[Dict[str, Any]] = None,
        limit: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """Execute N1QL query and return results"""
        from couchbase.options import QueryOptions

        try:
            # Execute query
            query_options = QueryOptions(named_parameters=params) if params else None
            result = self.cluster.query(query, query_options)

            # Yield results in chunks
            buffer = []
            for row in result:
                buffer.append(row)
                if len(buffer) >= self.chunk_size:
                    yield from buffer
                    buffer = []

            # Yield remaining items
            if buffer:
                yield from buffer

        except Exception as e:
            logger.error(f"Error executing Couchbase query: {e}")
            raise

    def load_documents(
        self,
        filter_: Optional[Dict[str, Any]] = None,
        limit: Optional[int] = None,
        projection: Optional[List[str]] = None,
        custom_query: Optional[str] = None,
    ) -> Iterator[TDataItem]:
        """Load documents from Couchbase collection"""
        try:
            if custom_query:
                # Use custom N1QL query
                query = custom_query
                params = {}
            else:
                # Build standard query
                where_clause, params = self._build_where_clause(filter_)
                order_clause = self._build_order_clause()

                # Build SELECT clause
                if projection:
                    select_fields = ", ".join([f"`{field}`" for field in projection])
                else:
                    select_fields = "*"

                # Build full query
                query_parts = [
                    f"SELECT {select_fields}",
                    f"FROM `{self.bucket_name}`.`{self.scope_name}`.`{self.collection_name}`",
                ]

                if where_clause:
                    query_parts.append(f"WHERE {where_clause}")

                if order_clause:
                    query_parts.append(order_clause)

                if limit:
                    query_parts.append(f"LIMIT {limit}")

                query = " ".join(query_parts)

            logger.info(f"Executing Couchbase query: {query}")

            # Execute query and yield results in chunks
            docs_buffer = []
            for doc in self._execute_query(query, params, limit):
                # Unwrap document if it's wrapped in collection name
                if isinstance(doc, dict) and self.collection_name in doc:
                    doc = doc[self.collection_name]

                docs_buffer.append(doc)

                if len(docs_buffer) >= self.chunk_size:
                    res = map_nested_in_place(convert_couchbase_objs, docs_buffer)
                    yield res
                    docs_buffer = []

            # Yield remaining documents
            if docs_buffer:
                res = map_nested_in_place(convert_couchbase_objs, docs_buffer)
                yield res

        except Exception as e:
            logger.error(f"Error loading documents from Couchbase: {e}")
            raise


class CouchbaseKVLoader:
    """Key-Value loader for Couchbase collections using KV operations"""

    def __init__(
        self,
        cluster: TCluster,
        bucket_name: str,
        scope_name: str,
        collection_name: str,
        chunk_size: int,
    ) -> None:
        self.cluster = cluster
        self.bucket_name = bucket_name
        self.scope_name = scope_name
        self.collection_name = collection_name
        self.chunk_size = chunk_size

        # Get bucket, scope, and collection references
        self.bucket = cluster.bucket(bucket_name)
        self.scope = self.bucket.scope(scope_name)
        self.collection = self.scope.collection(collection_name)

    def load_by_keys(
        self,
        keys: List[str],
        projection: Optional[List[str]] = None,
    ) -> Iterator[TDataItem]:
        """Load documents by keys using KV operations"""
        from couchbase.exceptions import DocumentNotFoundException

        docs_buffer = []

        for key in keys:
            try:
                # Get document by key
                result = self.collection.get(key)
                doc = result.content_as[dict]

                # Add the key to document
                doc["_id"] = key

                # Apply projection if specified
                if projection:
                    doc = {k: v for k, v in doc.items() if k in projection or k == "_id"}

                docs_buffer.append(doc)

                if len(docs_buffer) >= self.chunk_size:
                    res = map_nested_in_place(convert_couchbase_objs, docs_buffer)
                    yield res
                    docs_buffer = []

            except DocumentNotFoundException:
                logger.warning(f"Document with key {key} not found")
                continue
            except Exception as e:
                logger.error(f"Error loading document with key {key}: {e}")
                raise

        # Yield remaining documents
        if docs_buffer:
            res = map_nested_in_place(convert_couchbase_objs, docs_buffer)
            yield res


def collection_documents(
    cluster: TCluster,
    bucket_name: str,
    scope_name: str,
    collection_name: str,
    filter_: Optional[Dict[str, Any]] = None,
    projection: Optional[List[str]] = None,
    incremental: Optional[dlt.sources.incremental[Any]] = None,
    limit: Optional[int] = None,
    chunk_size: Optional[int] = 10000,
    custom_query: Optional[str] = None,
    kv_mode: bool = False,
    document_keys: Optional[List[str]] = None,
) -> Iterator[TDataItem]:
    """
    A DLT source which loads data from a Couchbase database.

    Args:
        cluster (Cluster): The Couchbase cluster instance.
        bucket_name (str): The bucket name.
        scope_name (str): The scope name.
        collection_name (str): The collection name.
        filter_ (Optional[Dict[str, Any]]): The filter to apply to the collection.
        projection (Optional[List[str]]): The fields to select.
        incremental (Optional[dlt.sources.incremental[Any]]): The incremental configuration.
        limit (Optional[int]): The maximum number of documents to load.
        chunk_size (Optional[int]): The number of documents to load in each batch.
        custom_query (Optional[str]): Custom N1QL query to execute instead of building one.
        kv_mode (bool): Use Key-Value operations instead of N1QL queries.
        document_keys (Optional[List[str]]): List of document keys to load in KV mode.

    Returns:
        Iterator[TDataItem]: An iterator of the loaded documents.
    """
    if kv_mode:
        if not document_keys:
            raise ValueError("document_keys parameter is required when kv_mode is True")

        loader = CouchbaseKVLoader(
            cluster, bucket_name, scope_name, collection_name, chunk_size
        )
        yield from loader.load_by_keys(keys=document_keys, projection=projection)
    else:
        loader = CouchbaseLoader(
            cluster, bucket_name, scope_name, collection_name, chunk_size, incremental
        )
        yield from loader.load_documents(
            filter_=filter_, limit=limit, projection=projection, custom_query=custom_query
        )


def convert_couchbase_objs(value: Any) -> Any:
    """Couchbase to dlt type conversion"""
    if isinstance(value, datetime):
        return ensure_pendulum_datetime(value)
    if isinstance(value, timedelta):
        return value.total_seconds()

    return value


def cluster_from_credentials(
    connection_string: str,
    username: str,
    password: str,
    **options: Any,
) -> TCluster:
    """Create Couchbase cluster connection from credentials"""
    from couchbase.auth import PasswordAuthenticator
    from couchbase.cluster import Cluster
    from couchbase.options import ClusterOptions

    # Create authenticator
    auth = PasswordAuthenticator(username, password)

    # Create cluster options
    cluster_options = ClusterOptions(auth)

    # Apply additional options if provided
    for key, value in options.items():
        if hasattr(cluster_options, key):
            setattr(cluster_options, key, value)

    # Connect to cluster
    cluster = Cluster(connection_string, cluster_options)

    # Wait until the cluster is ready
    cluster.wait_until_ready(timedelta(seconds=10))

    return cluster


def parse_couchbase_uri(uri: str) -> Tuple[str, str, str, Optional[str], Optional[str], Optional[str]]:
    """
    Parse Couchbase URI into components.

    Format: couchbase://username:password@host:port/bucket?scope=scope_name&collection=collection_name
    or: couchbases://username:password@host:port/bucket?scope=scope_name&collection=collection_name

    Returns:
        Tuple of (connection_string, username, password, bucket, scope, collection)
    """
    import re
    from urllib.parse import parse_qs, urlparse

    parsed = urlparse(uri)

    # Extract credentials
    username = parsed.username or ""
    password = parsed.password or ""

    # Build connection string (without credentials)
    scheme = parsed.scheme
    hostname = parsed.hostname or "localhost"
    port = parsed.port or 11210 if scheme == "couchbase" else 11207

    connection_string = f"{scheme}://{hostname}:{port}"

    # Extract bucket from path
    bucket = parsed.path.lstrip("/") if parsed.path else None

    # Extract scope and collection from query params
    query_params = parse_qs(parsed.query)
    scope = query_params.get("scope", [None])[0]
    collection = query_params.get("collection", [None])[0]

    return connection_string, username, password, bucket, scope, collection


@configspec
class CouchbaseCollectionConfiguration(BaseConfiguration):
    incremental: Optional[dlt.sources.incremental] = None  # type: ignore[type-arg]


@configspec
class CouchbaseCollectionResourceConfiguration(BaseConfiguration):
    connection_string: str = dlt.config.value
    username: dlt.TSecretValue = dlt.secrets.value
    password: dlt.TSecretValue = dlt.secrets.value
    bucket: str = dlt.config.value
    scope: Optional[str] = dlt.config.value
    collection: str = dlt.config.value
    incremental: Optional[dlt.sources.incremental] = None  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value
    projection: Optional[List[str]] = dlt.config.value


__source_name__ = "couchbase"
