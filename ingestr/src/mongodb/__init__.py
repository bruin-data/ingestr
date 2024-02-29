"""Source that loads collections form any a mongo database, supports incremental loads."""

from typing import Any, Iterable, List, Optional

import dlt
from dlt.sources import DltResource

from .helpers import (
    MongoDbCollectionConfiguration,
    MongoDbCollectionResourceConfiguration,
    client_from_credentials,
    collection_documents,
)


@dlt.source
def mongodb(
    connection_url: str = dlt.secrets.value,
    database: Optional[str] = dlt.config.value,
    collection_names: Optional[List[str]] = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    parallel: Optional[bool] = dlt.config.value,
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
        )(client, collection, incremental=incremental, parallel=parallel)


@dlt.common.configuration.with_config(
    sections=("sources", "mongodb"), spec=MongoDbCollectionResourceConfiguration
)
def mongodb_collection(
    connection_url: str = dlt.secrets.value,
    database: Optional[str] = dlt.config.value,
    collection: str = dlt.config.value,
    incremental: Optional[dlt.sources.incremental] = None,  # type: ignore[type-arg]
    write_disposition: Optional[str] = dlt.config.value,
    parallel: Optional[bool] = dlt.config.value,
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
    )(client, collection_obj, incremental=incremental, parallel=parallel)
