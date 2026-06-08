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

To read from Kinesis, you need an AWS access key pair with permission to describe the stream and read its records. The recommended approach is to create a dedicated IAM user:

1. Sign in to the [AWS IAM console](https://console.aws.amazon.com/iam/).
2. Go to **Users → Create user**, give the user a name (e.g. `ingestr-kinesis`), and continue without granting console access.
3. On the **Set permissions** step, choose **Attach policies directly** and attach a policy that allows reading the streams you want to ingest. The AWS-managed policy `AmazonKinesisReadOnlyAccess` is sufficient for most cases, or you can create a custom policy granting at minimum:
   - `kinesis:DescribeStream`
   - `kinesis:DescribeStreamSummary`
   - `kinesis:GetShardIterator`
   - `kinesis:GetRecords`
   - `kinesis:ListShards`
   - `kinesis:ListStreams`
4. Finish creating the user, then open the user and go to the **Security credentials** tab → **Access keys** → **Create access key**.
5. Select **Application running outside AWS**, create the key, and copy the **Access key ID** (your `aws_access_key_id`) and **Secret access key** (your `aws_secret_access_key`).
6. Note the AWS region where your stream lives (e.g. `eu-central-1`); that is your `region_name`. You can find it in the [Kinesis console](https://console.aws.amazon.com/kinesis/).

Once you have these credentials, let's say your `aws_access_key_id` is id_123, your `aws_secret_access_key` is secret_123 and your `region_name` is eu-central-1, here's a sample command that will copy the data from Kinesis into a DuckDB database:

```bash
ingestr ingest --source-uri 'kinesis://?aws_access_key_id=id_123&aws_secret_access_key=secret_123&region_name=eu-central-1' \
 --source-table 'stream_name_1' \
 --dest-uri duckdb:///kinesis.duckdb \
 --dest-table 'dest.results'
```

When using Kinesis as a source, specify the [StreamName] you want to read from as the `--source-table` parameter. For example, if you want to read from a Kinesis stream named "customer_events", you would use `--source-table 'customer_events'`.
You can also use a full Kinesis [StreamARN] to address the stream in [ARN] format, like `arn:aws:kinesis:eu-central-1:842404475894:stream/customer_events`.

### Initial Load Configuration
By default, ingestr reads from the beginning of the Kinesis stream. To start reading from a specific time, use the `interval_start` parameter.


[ARN]: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html
[StreamARN]: https://docs.aws.amazon.com/kinesis/latest/APIReference/API_StreamDescription.html#Streams-Type-StreamDescription-StreamARN
[StreamName]: https://docs.aws.amazon.com/kinesis/latest/APIReference/API_StreamDescription.html#Streams-Type-StreamDescription-StreamName
