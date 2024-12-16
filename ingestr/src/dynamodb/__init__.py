from dataclasses import dataclass
from typing import Optional

import boto3
import dlt
from boto3.dynamodb.conditions import Attr
from dlt.common.configuration.specs import AwsCredentials

PAGINATION_KEY = "LastEvaluatedKey"
FILTER_KEY = "FilterExpression"
DATA_KEY = "Items"


@dataclass
class TableSchema:
    primary_key: Optional[str]
    sort_key: Optional[str]


def parseSchema(table) -> TableSchema:
    schema = TableSchema(None, None)
    for key in table.key_schema:
        match key["KeyType"]:
            case "HASH":
                schema.primary_key = key["AttributeName"]
            case "RANGE":
                schema.sort_key = key["AttributeName"]

    if schema.primary_key is None:
        raise ValueError(f"Table {table.name} has no primary key!")

    return schema


@dlt.source(name="dynamodb")
def dynamodb_source(
    table_name: str,
    credentials: AwsCredentials,
    incremental: Optional[dlt.sources.incremental] = None,
):
    sesh = boto3.Session(
        aws_access_key_id=credentials.aws_access_key_id,
        aws_secret_access_key=credentials.aws_secret_access_key,
        region_name=credentials.region_name,
    )
    db = sesh.resource("dynamodb")
    table = db.Table(table_name)
    schema = parseSchema(table)
    resource = dlt.resource(
        dynamodb_table,
        primary_key=schema.primary_key,
    )

    yield resource(table, incremental)


def dynamodb_table(table, incremental: Optional[dlt.sources.incremental] = None):
    scan_args = {}
    if incremental and incremental.last_value:
        scan_args[FILTER_KEY] = Attr(incremental.cursor_path).gt(incremental.last_value)

    scan = table.scan(**scan_args)
    while True:
        yield from scan[DATA_KEY]
        if PAGINATION_KEY not in scan:
            break
        scan = table.scan(ExclusiveStartKey=scan[PAGINATION_KEY], **scan_args)
