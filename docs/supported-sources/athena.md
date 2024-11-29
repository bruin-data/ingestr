# Athena
[Athena](https://aws.amazon.com/athena/) is an interactive query service that allows users to analyze data directly in Amazon S3 using standard SQL.

The Athena destination stores data as Parquet files in S3 buckets and creates external tables in AWS Athena.

ingestr supports Athena as a destination.

## URI format
The URI format for Athena is as follows:

```plaintext
athena://?bucket=<s3://your-destination-bucket> \
    query_result_path=<s3://your-query-results-bucket> \
    aws_access_key_id=<your-aws-access-key-id> \
    aws_secret_access_key=<your-aws-secret-access-key> \
    athena_work_group=<your-athena-work-group> \
    region_name=<us-west-2>
```
URI parameters:

- `bucket`: The name of the bucket where the data will be stored which will contain the Parquet files that Athena will work with. For example,  s3://your_bucket_name
- `query_result_path`: The Amazon S3 bucket URL where the results of Athena queries will be saved. For example, s3://results_bucket_name
- `access_key_id` and `secret_access_key`: These are AWS credentials that will be used to authenticate with AWS services like S3 and Athena.
- `work_group`: The name of the Athena workgroup. For example,  my_group
- `region_name`: The AWS region of the Athena service and S3 buckets, where the queries will be executed, and data will be stored. For example, eu-central-1.

## Setting up an Athena Integration
Athena requires a `bucket_url`, `query_result_url`, `access_key_id`, `secret_access_key`, `work_group`, and `region_name` to access the S3 bucket. Please follow the guide on dltHub to obtain [credentials](https://dlthub.com/docs/dlt-ecosystem/destinations/athena#2-setup-bucket-storage-and-athena-credentials). Once you've completed the guide, you should have all the above-mentioned credentials.
```
ingestr ingest --source-uri 'chess://?players=max2,peter23' --source-table 'games' --dest-uri 'athena://?bucket_url=s3://bucket1&query_result_path=s3://bucket2&access_key_id=key123&secret_access_key=secret123&work_group=my_group&region_name=eu-central-1' --dest-table 'players.games'
```
This is a sample command that will copy the data from the Chess data source into Athena.

<img alt="athena_img" src="../media/athena.png" />
