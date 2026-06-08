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
- `bucket` (required): The name of the bucket where the data will be stored, containing the Parquet files that Athena will work with, e.g. `your_bucket_name` or `s3://your_bucket_name`.
- `access_key_id` and `secret_access_key` (required): These are AWS credentials that will be used to authenticate with AWS services like S3 and Athena.
- `session_token` (optional): The session token for temporary credentials.
- `region_name` (required if there's no local profile found): The AWS region of the Athena service and S3 buckets, e.g. `eu-central-1`
- `workgroup` (optional): The name of the Athena workgroup, e.g. `my_group`
- `profile` (optional): The name of the AWS profile to use, e.g. `my_profile`

You have two ways of providing credentials:
1. Provide `access_key_id` and `secret_access_key` directly in the URI.
2. Provide the name of the AWS profile to use in the `profile` parameter.

If there's no access key and secret key provided, ingestr will try to find the credentials in the local AWS credentials file.

## Setting up an Athena Integration

Athena requires an S3 bucket, an AWS region, a workgroup with a configured query result location, and credentials.

### Step 1: Create an S3 Bucket

1. Go to the [Amazon S3 Console](https://s3.console.aws.amazon.com/)
2. Click **Create bucket**
3. Enter a bucket name (e.g., "my-athena-results")
4. Select the same region where you'll use Athena
5. Click **Create bucket**

This is your `bucket` URI parameter. The region you picked is your `region_name`.

### Step 2: Configure an Athena Workgroup

Athena needs a **Query result location** configured on the workgroup it runs queries through. ingestr uses the account's `primary` workgroup by default.

1. Go to the [Athena Console](https://console.aws.amazon.com/athena/) → **Workgroups**
2. Either edit the existing `primary` workgroup or create a new one
3. Set **Query result location** to a prefix inside the bucket from Step 1 (e.g. `s3://my-athena-results/query-results/`) and save
4. If you created a new workgroup instead of using `primary`, pass its name via the `workgroup` URI parameter

### Step 3: Permissions

The identity ingestr uses must have:
- **S3** on the bucket: `GetObject`, `PutObject`, `DeleteObject`, `ListBucket`. The managed `AmazonS3FullAccess` policy works for testing; a bucket-scoped policy is safer for production.
- **Athena**: `StartQueryExecution`, `GetQueryExecution`, `GetQueryResults`, `StopQueryExecution` (or the managed `AmazonAthenaFullAccess` policy).
- **Glue**: `GetDatabase`, `CreateDatabase`, `GetTable`, `CreateTable`, `UpdateTable`, `DeleteTable`, `GetPartitions`, `BatchCreatePartition` — required so ingestr can register external tables in the AWS Glue Data Catalog that Athena reads from.

### Step 4: Credentials

Pick one of the following based on how you sign in to AWS:

**A. IAM user with long-lived keys** — for accounts not managed by AWS Identity Center, or when you want a dedicated service identity for ingestr.

1. In the [IAM Console](https://console.aws.amazon.com/iam/), go to **Users → Create user**, give it a name (e.g. "athena-integration"), and continue without granting console access
2. On the **Set permissions** step, attach a policy granting the S3, Athena, and Glue permissions from Step 3
3. Open the new user, go to the **Security credentials** tab → **Access keys** → **Create access key**, choose **Application running outside AWS**, and copy the **Access key ID** and **Secret access key** (the secret key is shown only once)

Pass these as `access_key_id` and `secret_access_key` in the URI.

**B. AWS Identity Center (SSO) via a local profile** — for developer machines that already use `aws configure sso` / `aws sso login`. ingestr reads the credentials (including the rotating session token) from your local AWS credentials file, so you never paste secrets into the URI.

1. If you have not yet, run `aws configure sso` and attach your Identity Center login to a named profile (e.g. `ingestr`)
2. Run `aws sso login --profile ingestr` whenever the session expires
3. Make sure the Identity Center permission set you assume includes the S3, Athena, and Glue permissions from Step 3
4. Pass `profile=ingestr` in the URI instead of access key parameters

**C. Short-lived keys from the SSO access portal** — for a one-off run from a machine that does not have the AWS CLI configured.

1. Open your AWS access portal (`https://<your-sso-portal>.awsapps.com/start`)
2. Pick the target account, then click **Access keys** next to the permission set you want to use
3. Copy the **Access key ID**, **Secret access key**, and **Session token**, and pass all three (`access_key_id`, `secret_access_key`, `session_token`) in the URI

> [!WARNING]
> Option C credentials expire after a few hours — do not use them for scheduled pipelines. Use Option A or B instead.

Once you have all the credentials:
```
ingestr ingest \
  --source-uri "stripe://?api_key=key123" \
  --source-table 'event' \
  --dest-uri "athena://?bucket=bucket_123&access_key_id=access_123&secret_access_key=secret_123&region_name=eu-central-1" \
  --dest-table 'stripe.event'
```
This is a sample command that will copy the data from the Stripe source into Athena.

<img alt="athena_img" src="../media/athena.png" />
