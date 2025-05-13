from typing import Optional

import dlt

from elasticsearch import Elasticsearch


@dlt.source
def elasticsearch_source(
    connection_url: str,
    index: str,
    verify_certs: bool,
    incremental: Optional[dlt.sources.incremental] = None,
):
    client = Elasticsearch(connection_url, verify_certs=verify_certs)

    @dlt.resource(primary_key="id", write_disposition="merge", incremental=incremental,columns={"updated_at": {"data_type": "date"}} )
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
            yield {"id": doc["_id"], **doc["_source"]}

        while True:
            # fetching page 2 and other pages (via .scroll)
            page = client.scroll(scroll_id=sid, scroll="5m")
            sid = page["_scroll_id"]
            hits = page["hits"]["hits"]
            if not hits:
                break
            for doc in hits:
                yield {"id": doc["_id"], **doc["_source"]}
            
        client.clear_scroll(scroll_id=sid)

    return get_documents
