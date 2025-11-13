"""Elasticsearch destination helpers"""

import json
import logging
from typing import Any, Dict, Iterator
from urllib.parse import urlparse

import dlt

from elasticsearch import Elasticsearch
from elasticsearch.helpers import bulk

# Suppress Elasticsearch transport logging
logging.getLogger("elasticsearch.transport").setLevel(logging.WARNING)
logging.getLogger("elastic_transport.transport").setLevel(logging.WARNING)


def process_file_items(file_path: str) -> Iterator[Dict[str, Any]]:
    """Process items from a file path (JSONL format)."""
    with open(file_path, "r") as f:
        for line in f:
            if line.strip():
                doc = json.loads(line.strip())
                # Clean DLT metadata
                cleaned_doc = {
                    k: v for k, v in doc.items() if not k.startswith("_dlt_")
                }
                yield cleaned_doc


def process_iterable_items(items: Any) -> Iterator[Dict[str, Any]]:
    """Process items from an iterable."""
    for item in items:
        if isinstance(item, dict):
            # Clean DLT metadata
            cleaned_item = {k: v for k, v in item.items() if not k.startswith("_dlt_")}
            yield cleaned_item


@dlt.destination(
    name="elasticsearch",
    loader_file_format="typed-jsonl",
    batch_size=1000,
    naming_convention="snake_case",
)
def elasticsearch_insert(
    items, table, connection_string: str = dlt.secrets.value
) -> None:
    """Insert data into Elasticsearch index.

    Args:
        items: Data items (file path or iterable)
        table: Table metadata containing name and schema info
        connection_string: Elasticsearch connection string
    """
    # Parse connection string
    parsed = urlparse(connection_string)

    # Build Elasticsearch client configuration
    actual_url = connection_string
    secure = True  # Default to HTTPS (secure by default)

    if connection_string.startswith("elasticsearch://"):
        actual_url = connection_string.replace("elasticsearch://", "")

        # Parse to check for query parameters
        temp_parsed = urlparse("http://" + actual_url)
        from urllib.parse import parse_qs

        query_params = parse_qs(temp_parsed.query)

        # Check ?secure parameter (defaults to true)
        if "secure" in query_params:
            secure = query_params["secure"][0].lower() in ["true", "1", "yes"]

        # Remove query params from URL for ES client
        actual_url = actual_url.split("?")[0]

        # Add scheme
        scheme = "https" if secure else "http"
        actual_url = f"{scheme}://{actual_url}"

        parsed = urlparse(actual_url)

    es_config: Dict[str, Any] = {
        "hosts": [actual_url],
        "verify_certs": secure,
        "ssl_show_warn": False,
    }

    # Add authentication if present
    if parsed.username and parsed.password:
        es_config["http_auth"] = (parsed.username, parsed.password)

    # Get index name from table metadata
    index_name = table["name"]

    # Connect to Elasticsearch
    client = Elasticsearch(**es_config)

    # Replace mode: delete existing index if it exists
    if client.indices.exists(index=index_name):
        client.indices.delete(index=index_name)

    # Process and insert documents
    if isinstance(items, str):
        documents = process_file_items(items)
    else:
        documents = process_iterable_items(items)

    # Prepare documents for bulk insert as generator
    def doc_generator():
        for doc in documents:
            es_doc: Dict[str, Any] = {"_index": index_name, "_source": doc.copy()}

            # Use _id if present, otherwise let ES generate one
            if "_id" in doc:
                es_doc["_id"] = str(doc["_id"])
                # Remove _id from source since it's metadata
                if "_id" in es_doc["_source"]:
                    del es_doc["_source"]["_id"]
            elif "id" in doc:
                es_doc["_id"] = str(doc["id"])

            yield es_doc

    # Bulk insert
    try:
        _, failed_items = bulk(client, doc_generator(), request_timeout=60)
        if failed_items:
            failed_count = (
                len(failed_items) if isinstance(failed_items, list) else failed_items
            )
            raise Exception(
                f"Failed to insert {failed_count} documents: {failed_items}"
            )
    except Exception as e:
        raise Exception(f"Elasticsearch bulk insert failed: {str(e)}")
