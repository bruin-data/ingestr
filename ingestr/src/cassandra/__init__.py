import dlt

from cassandra.auth import PlainTextAuthProvider  # type: ignore
from cassandra.cluster import Cluster  # type: ignore


@dlt.source(max_table_nesting=0)
def cassandra_source(
    host: str,
    port: int,
    keyspace: str | None = None,
    table: str | None = None,
    username: str | None = None,
    password: str | None = None,
):
    @dlt.resource()
    def fetch_data():
        auth_provider = PlainTextAuthProvider(username=username, password=password)
        cluster = Cluster(contact_points=[host], port=port, auth_provider=auth_provider)
        session = cluster.connect(keyspace)

        rows = session.execute(f"SELECT * FROM {table}")

        for row in rows:
            yield row

    return fetch_data
