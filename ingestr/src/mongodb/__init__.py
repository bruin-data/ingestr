"""Source that loads collections form any a mongo database, supports incremental loads."""

from typing import Any, Dict, Iterable, List, Mapping, Optional, Union

import dlt
from dlt.common.data_writers import TDataItemFormat
from dlt.sources import DltResource

from .helpers import (
    MongoDbCollectionConfiguration,
    MongoDbCollectionResourceConfiguration,
    client_from_credentials,
    collection_documents,
)


@dlt.source(max_table_nesting=0)
def mongodb(
    connection_url: str = dlt.secrets.value,
    database: Optional[str] = dlt.config.value,
    collection_names: Optional[List[str]] = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    parallel: Optional[bool] = dlt.config.value,
    limit: Optional[int] = None,
    filter_: Optional[Dict[str, Any]] = None,
    projection: Optional[Union[Mapping[str, Any], Iterable[str]]] = None,
    pymongoarrow_schema: Optional[Any] = None,
) -> Iterable[DltResource]:
    """
    A DLT source which loads data from a mongo database using PyMongo.
    Resources are automatically created for each collection in the database or from the given list of collection.

    Args:
        connection_url (str): Database connection_url.
        database (Optional[str]): Selected database name, it will use the default database if not passed.
        collection_names (Optional[List[str]]): The list of collections `pymongo.collection.Collection` to load.
        incremental (Optional[dlt.sources.incremental]): Option to enable incremental loading for the collection.
            E.g., `incremental=dlt.sources.incremental('updated_at', pendulum.parse('2022-01-01T00:00:00Z'))`
        write_disposition (str): Write disposition of the resource.
        parallel (Optional[bool]): Option to enable parallel loading for the collection. Default is False.
        limit (Optional[int]):
            The maximum number of documents to load. The limit is
            applied to each requested collection separately.
        filter_ (Optional[Dict[str, Any]]): The filter to apply to the collection.
        projection: (Optional[Union[Mapping[str, Any], Iterable[str]]]): The projection to select fields of a collection
            when loading the collection. Supported inputs:
                include (list) - ["year", "title"]
                include (dict) - {"year": True, "title": True}
                exclude (dict) - {"released": False, "runtime": False}
            Note: Can't mix include and exclude statements '{"title": True, "released": False}`
        pymongoarrow_schema (pymongoarrow.schema.Schema): Mapping of expected field types of a collection to convert BSON to Arrow

    Returns:
        Iterable[DltResource]: A list of DLT resources for each collection to be loaded.
    """

    # set up mongo client
    client = client_from_credentials(connection_url)
    if not database:
        mongo_database = client.get_default_database()
    else:
        mongo_database = client[database]

    # use provided collection or all conllections
    if not collection_names:
        collection_names = mongo_database.list_collection_names()

    collection_list = [mongo_database[collection] for collection in collection_names]

    for collection in collection_list:
        yield dlt.resource(  # type: ignore
            collection_documents,
            name=collection.name,
            primary_key="_id",
            write_disposition=write_disposition,
            spec=MongoDbCollectionConfiguration,
            max_table_nesting=0,
        )(
            client,
            collection,
            incremental=incremental,
            parallel=parallel,
            limit=limit,
            filter_=filter_ or {},
            projection=projection,
            pymongoarrow_schema=pymongoarrow_schema,
        )


@dlt.resource(
    name=lambda args: args["collection"],
    standalone=True,
    spec=MongoDbCollectionResourceConfiguration,
)
def mongodb_collection(
    connection_url: str = dlt.secrets.value,
    database: Optional[str] = dlt.config.value,
    collection: str = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    parallel: Optional[bool] = False,
    limit: Optional[int] = None,
    chunk_size: Optional[int] = 10000,
    data_item_format: Optional[TDataItemFormat] = "object",
    filter_: Optional[Dict[str, Any]] = None,
    projection: Optional[Union[Mapping[str, Any], Iterable[str]]] = dlt.config.value,
    pymongoarrow_schema: Optional[Any] = None,
) -> Any:
    """
    A DLT source which loads a collection from a mongo database using PyMongo.

    Args:
        connection_url (str): Database connection_url.
        database (Optional[str]): Selected database name, it will use the default database if not passed.
        collection (str): The collection name to load.
        incremental (Optional[dlt.sources.incremental]): Option to enable incremental loading for the collection.
            E.g., `incremental=dlt.sources.incremental('updated_at', pendulum.parse('2022-01-01T00:00:00Z'))`
        write_disposition (str): Write disposition of the resource.
        parallel (Optional[bool]): Option to enable parallel loading for the collection. Default is False.
        limit (Optional[int]): The number of documents load.
        chunk_size (Optional[int]): The number of documents load in each batch.
        data_item_format (Optional[TDataItemFormat]): The data format to use for loading.
            Supported formats:
                object - Python objects (dicts, lists).
                arrow - Apache Arrow tables.
        filter_ (Optional[Dict[str, Any]]): The filter to apply to the collection.
        projection: (Optional[Union[Mapping[str, Any], Iterable[str]]]): The projection to select fields
            when loading the collection. Supported inputs:
                include (list) - ["year", "title"]
                include (dict) - {"year": True, "title": True}
                exclude (dict) - {"released": False, "runtime": False}
            Note: Can't mix include and exclude statements '{"title": True, "released": False}`
        pymongoarrow_schema (pymongoarrow.schema.Schema): Mapping of expected field types to convert BSON to Arrow

    Returns:
        Iterable[DltResource]: A list of DLT resources for each collection to be loaded.
    """
    # set up mongo client
    client = client_from_credentials(connection_url)
    if not database:
        mongo_database = client.get_default_database()
    else:
        mongo_database = client[database]

    collection_obj = mongo_database[collection]

    return dlt.resource(  # type: ignore
        collection_documents,
        name=collection_obj.name,
        primary_key="_id",
        write_disposition=write_disposition,
    )(
        client,
        collection_obj,
        incremental=incremental,
        parallel=parallel,
        limit=limit,
        chunk_size=chunk_size,
        data_item_format=data_item_format,
        filter_=filter_ or {},
        projection=projection,
        pymongoarrow_schema=pymongoarrow_schema,
    )
