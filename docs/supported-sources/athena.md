# AWS Athena
[Athena](https://aws.amazon.com/athena/) is an interactive query service that allows users to analyze data directly in Amazon S3 using standard SQL.

The Athena destination stores data as Parquet files in S3 buckets and creates external tables in AWS Glue Catalog.

ingestr supports Athena as a destination.

## URI format
The URI format for Athena is as follows:

```plaintext
athena://?bucket=<your-destination-bucket> \
    access_key_id=<your-aws-access-key-id> \
    secret_access_key=<your-aws-secret-access-key> \
    region_name=<your-aws-region>
```
URI parameters:
- `bucket` (required): name of the S3 bucket where data will be stored, e.g. `my_bucket` or `s3://my_bucket`.
- `region_name` (required unless a local profile provides one): AWS region of the bucket and Athena, e.g. `eu-central-1`.
- `access_key_id` and `secret_access_key` (required unless `profile` is set): AWS credentials.
- `session_token` (optional): session token for temporary credentials.
- `workgroup` (optional): Athena workgroup name. Defaults to the account's `primary` workgroup.
- `profile` (optional): local AWS profile name (e.g. one configured via `aws configure sso`). When set, ingestr reads credentials from your local AWS credentials file instead of the URI.

## Setting up an Athena Integration

### 1. S3 bucket and Athena workgroup

1. Create an S3 bucket in the [S3 console](https://s3.console.aws.amazon.com/), e.g. `my-company-ingestr`. This is your `bucket` parameter.
2. Make sure a **Query result location** is configured on your Athena workgroup. ingestr defaults to `primary` — set its result location under **Athena → Workgroups → primary → Edit**, or create a dedicated workgroup and pass its name via `workgroup=`.

The region of your bucket is your `region_name`.

### 2. Permissions

The identity ingestr uses must have:
- **S3** on the bucket: `GetObject`, `PutObject`, `DeleteObject`, `ListBucket`.
- **Athena**: `StartQueryExecution`, `GetQueryExecution`, `GetQueryResults`, `StopQueryExecution` (or the managed `AmazonAthenaFullAccess`).
- **Glue**: `GetDatabase`, `CreateDatabase`, `GetTable`, `CreateTable`, `UpdateTable`, `DeleteTable`, `GetPartitions`, `BatchCreatePartition` — needed to register external tables in the Glue Data Catalog.

### 3. Credentials

Pick one:

**A. IAM user with long-lived keys.** In the [IAM console](https://console.aws.amazon.com/iam/), create a user, attach the policies from step 2, then under **Security credentials → Access keys** create a key.

```plaintext
athena://?bucket=my-company-ingestr&access_key_id=AKIA...&secret_access_key=...&region_name=eu-central-1
```

**B. AWS Identity Center (SSO) via a local profile.** Run `aws configure sso` once, then `aws sso login --profile <name>` whenever the session expires. ingestr reads credentials from your AWS credentials file.

```plaintext
athena://?bucket=my-company-ingestr&profile=ingestr&region_name=eu-central-1
```

**C. Short-lived keys from the SSO access portal.** Open `https://<your-sso-portal>.awsapps.com/start`, pick the account, click **Access keys**, and copy all three fields.

```plaintext
athena://?bucket=my-company-ingestr&access_key_id=ASIA...&secret_access_key=...&session_token=...&region_name=eu-central-1
```

> [!WARNING]
> Option C credentials expire after a few hours — do not use them for scheduled pipelines.

### Sample command

```
ingestr ingest \
  --source-uri "stripe://?api_key=key123" \
  --source-table 'event' \
  --dest-uri "athena://?bucket=bucket_123&access_key_id=access_123&secret_access_key=secret_123&region_name=eu-central-1" \
  --dest-table 'stripe.event'
```

<img alt="athena_img" src="../media/athena.png" />
