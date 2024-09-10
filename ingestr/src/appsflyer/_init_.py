from typing import Iterable

import dlt
from dlt.sources import DltResource

from ingestr.src.appsflyer.helpers import AppsflyerAPI

Client = AppsflyerAPI()


@dlt.source(max_table_nesting=0)
def appsflyer_source() -> Iterable[DltResource]:
    token = ""
    app_id = ""
    yield from Client.fetch_data(app_id, token)
