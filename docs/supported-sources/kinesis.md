# Amazon Kinesis

[Amazon Kinesis](https://docs.aws.amazon.com/streams/latest/dev/key-concepts.html) is a
cloud-based service for real-time data streaming and analytics, enabling the processing and analysis of large streams of data in real time.

ingestr supports Kinesis as a source.

## URI format
The URI format for Kinesis is as follows:

```plaintext
kinesis://?aws_access_key_id=<aws-access-key-id>&aws_secret_access_key=<aws-secret-access-key>&region_name=<region-name>
``` 

URI parameters:
- `aws_access_key_id`: the AWS access key ID used to authenticate the request
- `aws_secret_access_key`: the AWS secret access key used to authenticate the request
- `region_name`: the AWS region name where the stream is located



## Setting up a Kinesis Integration
To get Kinesis credentials, please refer to the guide [here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/amazon_kinesis#grab-credentials)

Once you complete the guide, you should have a aws_access_key_id, aws_secret_access_key and region_name. Let's say your `aws_access_key_id` is id_123, your `aws_secret_access_key` is secret_123 and your `region_name` is eu-central-1, here's a sample command that will copy the data from Kinesis into a DuckDB database:

```bash
ingestr ingest --source-uri 'kinesis://?aws_access_key_id=id_123&aws_secret_access_key=secret_123&region_name=eu-central-1' \
 --source-table 'stream_name_1' \
 --dest-uri duckdb:///kinesis.duckdb \
 --dest-table 'dest.results'
```

When using Kinesis as a source, specify the `stream name` you want to read from as the `--source-table` parameter. For example, if you want to read from a Kinesis stream named "customer_events", you would use `--source-table 'customer_events'`.

### Initial Load Configuration
By default, ingestr reads from the beginning of the Kinesis stream. To start reading from a specific time, use the `interval_start` parameter.


