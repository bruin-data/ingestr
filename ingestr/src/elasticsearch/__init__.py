import dlt

from elasticsearch import Elasticsearch


@dlt.source
def elasticsearch_source(connection_url: str, index: str, verify_certs: bool):
    client = Elasticsearch(connection_url, verify_certs=verify_certs)

    @dlt.resource(primary_key="id", write_disposition="merge")
    def get_documents():
        body = {"query": {"match_all": {}}}

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
