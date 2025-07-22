from typing import Any, Dict, Iterable

from influxdb_client import InfluxDBClient  # type: ignore


class InfluxClient:
    def __init__(
        self, url: str, token: str, org: str, bucket: str, verify_ssl: bool = True
    ) -> None:
        self.client = InfluxDBClient(
            url=url, token=token, org=org, verify_ssl=verify_ssl
        )
        self.bucket = bucket

    def fetch_measurement(
        self, measurement: str, start: str, end: str | None = None
    ) -> Iterable[Dict[str, Any]]:
        query = f'from(bucket: "{self.bucket}") |> range(start: {start}'
        if end:
            query += f", stop: {end}"
        query += f') |> filter(fn: (r) => r["_measurement"] == "{measurement}")'
        query_api = self.client.query_api()

        for record in query_api.query_stream(query):
            cleaned_record = {}
            exclude_keys = {"result", "table", "_start", "_stop"}
            for key, value in record.values.items():
                if key in exclude_keys:
                    continue
                if key.startswith("_"):
                    cleaned_record[key[1:]] = value
                else:
                    cleaned_record[key] = value
            yield cleaned_record
