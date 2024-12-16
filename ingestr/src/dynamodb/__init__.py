import dlt
import boto3
from boto3.dynamodb.conditions import Attr

from typing import Optional
from dlt.common.configuration.specs import AwsCredentials

PAGINATION_KEY = "LastEvaluatedKey"
FILTER_KEY = "FilterExpression"
DATA_KEY = "Items"

@dlt.source
def dynamodb_source(table: str, credentials: AwsCredentials, incremental: Optional[dlt.sources.incremental] = None):
    sesh = boto3.Session(
        aws_access_key_id=credentials.aws_access_key_id,
        aws_secret_access_key=credentials.aws_secret_access_key,
        region_name=credentials.region_name,
    )
    db = sesh.resource("dynamodb")

    # TODO: dynamically bind primary key
    yield scan_table(db.Table(table), incremental)

@dlt.resource(write_disposition="merge")
def scan_table(table, incremental: Optional[dlt.sources.incremental] = None):
    scan_args = {}
    if incremental and incremental.last_value:
        scan_args[FILTER_KEY] =  Attr(incremental.cursor_path).gt(incremental.last_value)

    scan = table.scan(**scan_args)
    while True:
        yield scan[DATA_KEY]
        if PAGINATION_KEY not in scan:
            break
        scan = table.scan(ExclusiveStartKey=scan[PAGINATION_KEY], **scan_args)
