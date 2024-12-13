import dlt
import boto3

from dlt.common.configuration.specs import AwsCredentials

@dlt.source
def dynamodb_source(table: str, credentials: AwsCredentials):
    sesh = boto3.Session(
        aws_access_key_id=credentials.aws_access_key_id,
        aws_secret_access_key=credentials.aws_secret_access_key,
        region_name=credentials.region_name,
    )
    db = sesh.resource("dynamodb")
    yield scan_table(db.Table(table))

@dlt.resource
def scan_table(table):
    yield table.scan()["Items"]
    