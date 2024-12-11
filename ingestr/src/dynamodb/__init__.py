import dlt
import boto3

@dlt.source
def dynamodb_source(session: boto3.Session):
    pass