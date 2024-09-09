from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from ingestr.src.appsflyer.helpers import AppsflyerAPI

@dlt.source(max_table_nesting=0)
def appsflyer_source(api_key: str) -> Iterable[DltResource]:
     return 1