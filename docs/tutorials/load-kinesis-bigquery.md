# Load Kinesis Data to BigQuery

Welcome! 👋 This tutorial will guide you through loading data from `Amazon Kinesis` into `Google BigQuery` using `ingestr`, a command-line tool that enables data ingestion between any source and destination using simple flags, no coding required.

Amazon Kinesis is a cloud-based service for real-time data streaming and analytics that processes large data streams in real-time. You often need to store this data in a data warehouse like BigQuery for analysis and reporting. This is where ingestr simplifies the process.

## Prerequisites
- Install ingestr by following the instructions [here](../getting-started/quickstart.md#Installation)
- AWS credentials - Access key and Secret key
- BigQuery service account

## Configuration Steps 
### Source Configuration - Kinesis

#### `--source-uri`
This flag connects to your Kinesis stream. The URI format is:

```bash
kinesis://?aws_access_key_id=$KEY_ID&aws_secret_access_key=$SECRET_KEY&region_name=eu-central-1
```

Required parameters:
- `aws_access_key_id`: Your AWS access key
- `aws_secret_access_key`: Your AWS secret key
- `region_name`: AWS region of your Kinesis stream.

#### `--source-table`
This flag specifies which Kinesis stream to read from:
```bash
--source-table 'kinesis_stream_name'
```

### Destination Configuration - BigQuery
#### `--dest-uri`

This flag connects to BigQuery. The URI format is:
```bash
bigquery://project-name?credentials_path=/path/to/service/account.json&location=<location>
```
Required parameters:
- `project-name`: Your BigQuery project name
- `credentials_path`: Path to service account JSON file
- `location`: (Optional) Dataset location

#### `--dest-table`
This flag specifies where to save the data:

```bash
--dest-table 'schema.table_name'
```

Now that we've configured all our flags, we can run a single command to connect to Kinesis, read from our specified stream, and load the data into our BigQuery target table.

```bash
ingestr ingest \
    --source-uri 'kinesis://?aws_access_key_id=id_123&aws_secret_access_key=secret_123&region_name=eu-central-1' \
    --source-table 'stream_name_1' \
    --dest-uri 'bigquery://test-playground?credentials_path=/Users/abc.json' \
    --dest-table 'dest.results'
```

After running this command, your Kinesis data will be loaded into BigQuery. Here's what the data looks like in the destination:

<img alt="kinesis_bigquery" src="../media/kinesis.bigquery.png" />

🎉 Congratulations!
You've successfully loaded data from Amazon Kinesis to your desired destination.
