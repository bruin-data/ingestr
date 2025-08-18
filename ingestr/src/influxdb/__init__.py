from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import InfluxClient


@dlt.source(max_table_nesting=0)
def influxdb_source(
    measurement: str,
    host: str,
    org: str,
    bucket: str,
    token: str = dlt.secrets.value,
    secure: bool = True,
    start_date: pendulum.DateTime = pendulum.datetime(2024, 1, 1),
    end_date: pendulum.DateTime | None = None,
) -> Iterable[DltResource]:
    client = InfluxClient(
        url=host, token=token, org=org, bucket=bucket, verify_ssl=secure
    )

    @dlt.resource(name=measurement)
    def fetch_table(
        timestamp=dlt.sources.incremental(
            "time",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterable[TDataItem]:
        if timestamp.last_value is None:
            start = start_date.isoformat()
        else:
            start = timestamp.last_value.isoformat()
        if timestamp.end_value is None:
            end = pendulum.now().isoformat()
        else:
            end = timestamp.end_value.isoformat()
        yield from client.fetch_measurement(measurement, start, end)

    return fetch_table
