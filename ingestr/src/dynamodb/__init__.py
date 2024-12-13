import dlt
import boto3

from dlt.common.configuration.specs import AwsCredentials

@dlt.source
def dynamodb_source(table: str, credentials: AwsCredentials):
    pass