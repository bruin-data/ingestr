from typing import Iterable

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from ingestr.src.appsflyer.client import AppsflyerClient


@dlt.source(max_table_nesting=0)
def appsflyer_source(
    api_key: str, start_date: str, end_date: str
) -> Iterable[DltResource]:
    client = AppsflyerClient(api_key)

    @dlt.resource(write_disposition="merge", merge_key="install_time")
    def campaigns() -> Iterable[TDataItem]:
        yield from client.fetch_campaigns(start_date, end_date)

    @dlt.resource(write_disposition="merge", merge_key="install_time")
    def creatives() -> Iterable[TDataItem]:
        yield from client.fetch_creatives(start_date, end_date)

    return campaigns, creatives
