import dlt

from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from typing import List, Iterable, Sequence
from .client import AppStoreConnectClient

@dlt.source
def app_store(
    key_id: str,
    key_path: str,
    issuer_id: str,
    app_ids: List[str],
) -> Sequence[DltResource]:
    key = None
    with open(key_path) as f: key = f.read()
    client = AppStoreConnectClient(
        key.encode(),
        key_id,
        issuer_id
    )

    return [
        app_downloads_detailed(client, app_ids)
    ]


@dlt.resource(name="app-downloads-detailed")
def app_downloads_detailed(client: AppStoreConnectClient, app_ids: List[str]) -> Iterable[TDataItem]:
    for app_id in app_ids:
        res = client.list_analytics_reports(app_id)
        print(res)