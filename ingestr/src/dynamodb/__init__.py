import dlt
import boto3

from dlt.common.configuration.specs import AwsCredentials

PAGINATION_KEY = "LastEvaluatedKey"

@dlt.source
def dynamodb_source(table: str, credentials: AwsCredentials):
    sesh = boto3.Session(
        aws_access_key_id=credentials.aws_access_key_id,
        aws_secret_access_key=credentials.aws_secret_access_key,
        region_name=credentials.region_name,
    )
    db = sesh.resource("dynamodb")
    yield scan_table(db.Table(table))

@dlt.resource(write_disposition="merge")
def scan_table(table):
    scan = table.scan()
    while True:
        yield scan["Items"]
        if PAGINATION_KEY not in scan:
            break
        scan = table.scan(ExclusiveStartKey=scan[PAGINATION_KEY])
    