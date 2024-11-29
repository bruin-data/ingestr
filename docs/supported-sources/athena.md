# Athena
[Athena](https://aws.amazon.com/athena/) is an interactive query service that allows users to analyze data directly in Amazon S3 using standard SQL.

The Athena destination stores data as Parquet files in S3 buckets and creates external tables in AWS Athena.

ingestr supports Athena as a destination.

## URI format
The URI format for Athena is as follows:

```plaintext
athena://?bucket=<your-destination-bucket> \
    query_result_path=<your-query-results-location> \
    access_key_id=<your-aws-access-key-id> \
    secret_access_key=<your-aws-secret-access-key> \
    work_group=<your-athena-work-group> \
    region_name=<your-aws-region>
```
URI parameters:
- `bucket`: The name of the bucket where the data will be stored, containing the Parquet files that Athena will work with. For example,`your_bucket_name` or s3://your_bucket_name.
- `query_result_path` (optional): The query location path where the results of Athena queries will be saved. For example, `dest_path` or s3://dest_path. If not provided, it will default to the bucket specified in the `bucket parameter`.
- `access_key_id` and `secret_access_key`: These are AWS credentials that will be used to authenticate with AWS services like S3 and Athena.
- `work_group`: The name of the Athena workgroup. For example, my_group
- `region_name`: The AWS region of the Athena service and S3 buckets. For example, eu-central-1.

## Setting up an Athena Integration
Athena requires a `bucket`, `query_result_path`, `access_key_id`, `secret_access_key`, `work_group`, and `region_name` to access the S3 bucket. Please follow the guide on dltHub to obtain [credentials](https://dlthub.com/docs/dlt-ecosystem/destinations/athena#2-setup-bucket-storage-and-athena-credentials). Once you've completed the guide, you should have all the above-mentioned credentials.

```
ingestr ingest \
  --source-uri "stripe://?api_key=key123" \
  --source-table 'event' \
  --dest-uri "athena://?bucket=bucket_123&query_result_path=path_123&access_key_id=access_123&secret_access_key=secret_123&work_group=my_group&region_name=eu-central-1" \
  --dest-table 'stripe.event'
```
This is a sample command that will copy the data from the Stripe source into Athena.

<img alt="athena_img" src="../media/athena.png" />
