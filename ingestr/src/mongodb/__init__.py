# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
    process_file_items,
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
    chunk_size: Optional[int] = 1000,
    data_item_format: Optional[TDataItemFormat] = "object",
    filter_: Optional[Dict[str, Any]] = None,
    projection: Optional[Union[Mapping[str, Any], Iterable[str]]] = dlt.config.value,
    pymongoarrow_schema: Optional[Any] = None,
    custom_query: Optional[List[Dict[str, Any]]] = None,
) -> DltResource:
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
        custom_query (Optional[List[Dict[str, Any]]]): Custom MongoDB aggregation pipeline to execute instead of find()

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
        custom_query=custom_query,
    )


def mongodb_insert(uri: str):
    """Creates a dlt.destination for inserting data into a MongoDB collection.

    Args:
        uri (str): MongoDB connection URI including database.

    Returns:
        dlt.destination: A DLT destination object configured for MongoDB.
    """
    from urllib.parse import urlparse

    parsed_uri = urlparse(uri)
    database = (
        parsed_uri.path.lstrip("/") if parsed_uri.path.lstrip("/") else "ingestr_db"
    )
    first_batch_per_table: dict[str, bool] = {}
    BATCH_SIZE = 10000

    def destination(items, table) -> None:
        import pyarrow
        from pymongo import MongoClient

        collection_name = table["name"]

        if collection_name not in first_batch_per_table:
            first_batch_per_table[collection_name] = True

        with MongoClient(uri) as client:
            db = client[database]
            collection = db[collection_name]

            # Process documents
            if isinstance(items, str):
                documents = process_file_items(items)
            elif isinstance(items, pyarrow.RecordBatch):
                documents = items.to_pylist()
            else:
                documents = [item for item in items if isinstance(item, dict)]

            write_disposition = table.get("write_disposition")

            batches = [
                documents[i : i + BATCH_SIZE]
                for i in range(0, len(documents), BATCH_SIZE)
            ]

            if write_disposition == "merge":
                from pymongo import ReplaceOne

                primary_keys = [
                    col_name
                    for col_name, col_def in table.get("columns", {}).items()
                    if isinstance(col_def, dict) and col_def.get("primary_key")
                ]

                if not primary_keys:
                    raise ValueError(
                        f"Merge operation requires primary keys for table '{collection_name}'. "
                        f"Please define primary keys in the table schema or use 'replace' write disposition."
                    )

                for batch in batches:
                    operations = [
                        ReplaceOne(
                            {key: doc[key] for key in primary_keys},
                            doc,
                            upsert=True,
                        )
                        for doc in batch
                        if all(key in doc for key in primary_keys)
                    ]
                    if operations:
                        collection.bulk_write(operations, ordered=False)

            elif write_disposition == "replace":
                if first_batch_per_table[collection_name] and documents:
                    collection.delete_many({})
                    first_batch_per_table[collection_name] = False

                for batch in batches:
                    if batch:
                        collection.insert_many(batch)

            else:
                raise ValueError(
                    f"Unsupported write disposition '{write_disposition}' for MongoDB destination. "
                )

    return dlt.destination(
        destination,
        name="mongodb",
        loader_file_format="typed-jsonl",
        batch_size=1000,
        naming_convention="snake_case",
        loader_parallelism_strategy="sequential",
    )
