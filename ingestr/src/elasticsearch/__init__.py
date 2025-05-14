from datetime import date, datetime
from typing import Any, Optional

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from pendulum import parse

from elasticsearch import Elasticsearch


@dlt.source
def elasticsearch_source(
    connection_url: str,
    index: str,
    verify_certs: bool,
    incremental: Optional[dlt.sources.incremental] = None,
):
    client = Elasticsearch(connection_url, verify_certs=verify_certs)

    @dlt.resource(
        name=index, primary_key="id", write_disposition="merge", incremental=incremental
    )
    def get_documents(incremental=incremental):
        body = {"query": {"match_all": {}}}

        if incremental:
            start_value = incremental.last_value
            range_filter = {"gte": start_value}
            if incremental.end_value is not None:
                range_filter["lt"] = incremental.end_value
            body = {"query": {"range": {incremental.cursor_path: range_filter}}}

        page = client.search(index=index, scroll="5m", size=5, body=body)

        sid = page["_scroll_id"]
        hits = page["hits"]["hits"]

        if not hits:
            return

        # fetching first page (via .search)
        for doc in hits:
            doc_data = {"id": doc["_id"], **doc["_source"]}
            if incremental:
                doc_data[incremental.cursor_path] = convert_elasticsearch_objs(
                    doc_data[incremental.cursor_path]
                )
            yield doc_data

        while True:
            # fetching page 2 and other pages (via .scroll)
            page = client.scroll(scroll_id=sid, scroll="5m")
            sid = page["_scroll_id"]
            hits = page["hits"]["hits"]
            if not hits:
                break
            for doc in hits:
                doc_data = {"id": doc["_id"], **doc["_source"]}
                if incremental:
                    doc_data[incremental.cursor_path] = convert_elasticsearch_objs(
                        doc_data[incremental.cursor_path]
                    )
                yield doc_data

        client.clear_scroll(scroll_id=sid)

    return get_documents


def convert_elasticsearch_objs(value: Any) -> Any:
    if isinstance(value, str):
        parsed_date = parse(value, strict=False)
        if parsed_date is not None:
            if isinstance(
                parsed_date,
                (pendulum.DateTime, pendulum.Date, datetime, date, str, float, int),
            ):
                return ensure_pendulum_datetime(parsed_date)
    return value
