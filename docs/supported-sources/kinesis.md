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

To access Amazon Kinesis, you need AWS credentials with appropriate permissions.

### Step 1: Create an IAM User (if needed)

1. Go to the [AWS IAM Console](https://console.aws.amazon.com/iam/)
2. Click **Users** → **Create user**
3. Enter a username (e.g., "kinesis-integration") and continue without granting console access
4. Click **Next**

### Step 2: Assign Permissions

Attach a policy that grants Kinesis read access. You can use the managed policy `AmazonKinesisReadOnlyAccess` or create a custom policy with:
- `kinesis:DescribeStream`
- `kinesis:DescribeStreamSummary`
- `kinesis:GetShardIterator`
- `kinesis:GetRecords`
- `kinesis:ListShards`
- `kinesis:ListStreams`

### Step 3: Get Access Keys

1. Open the new user and go to the **Security credentials** tab
2. Under **Access keys**, click **Create access key**
3. Select **Application running outside AWS** and create the key
4. Copy the **Access key ID** and **Secret access key** (the secret key is shown only once)

### Finding Your Region

Your region is the AWS region where your Kinesis stream is located (e.g., `us-east-1`, `eu-central-1`, `ap-southeast-1`).

Once you have your `aws_access_key_id`, `aws_secret_access_key`, and `region_name`, let's say your `aws_access_key_id` is id_123, your `aws_secret_access_key` is secret_123 and your `region_name` is eu-central-1, here's a sample command that will copy the data from Kinesis into a DuckDB database:

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
