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
- `access_key_id` and `secret_access_key` (required unless `profile` is set): AWS credentials used to authenticate with S3, Athena, and Glue.
- `session_token` (optional): Session token for temporary credentials (e.g. credentials copied from the AWS Identity Center access portal).
- `region_name` (required if there's no local profile found): The AWS region of the Athena service and S3 buckets, e.g. `eu-central-1`.
- `workgroup` (optional): The name of the Athena workgroup. Defaults to the account's `primary` workgroup if not provided.
- `profile` (optional): The name of a local AWS profile to use (e.g. one configured via `aws configure sso`). When set, ingestr reads credentials from your local AWS credentials file instead of from the URI.

There are three ways to provide credentials, covered in detail under [Setting up an Athena Integration](#setting-up-an-athena-integration) below: a dedicated IAM user with long-lived keys, an AWS Identity Center (SSO) login via a local profile, or short-lived keys copied from the SSO access portal.

## Setting up an Athena Integration
Athena requires a `bucket`, a `region_name`, and AWS credentials. The bucket is used to store both the data files written by ingestr and the query results that Athena produces.

### 1. Storage and permissions (always required)

These steps are the same no matter how you authenticate.

1. **Create an S3 bucket** for ingestr data in the same AWS region you plan to query from. In the [S3 console](https://s3.console.aws.amazon.com/), click **Create bucket**, choose a unique name (e.g. `my-company-ingestr`), keep "Block all public access" enabled, and create it. This name is the `bucket` URI parameter.
2. **Create an Athena workgroup** (optional). AWS already provides a default workgroup called `primary`. If you do not pass `workgroup=` in the URI, ingestr uses `primary` — just make sure it has a **Query result location** configured under **Athena → Workgroups → primary → Edit**. If you prefer to isolate ingestr's queries, create a new workgroup with its own query result location (e.g. `s3://my-company-ingestr/athena-results/`) and pass the workgroup name as the `workgroup` URI parameter.
3. **Note the region** of your bucket and Athena workgroup, for example `eu-central-1`. This is the `region_name` URI parameter.
4. **Decide what permissions ingestr will need.** Whichever identity you use in the next step must be allowed to:
   - `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, `s3:ListBucket` on your bucket and its objects (the AWS-managed `AmazonS3FullAccess` works for testing, but a bucket-scoped policy is safer).
   - `athena:StartQueryExecution`, `athena:GetQueryExecution`, `athena:GetQueryResults`, `athena:StopQueryExecution` (or the managed `AmazonAthenaFullAccess` policy).
   - `glue:GetDatabase`, `glue:CreateDatabase`, `glue:GetTable`, `glue:CreateTable`, `glue:UpdateTable`, `glue:DeleteTable`, `glue:GetPartitions`, `glue:BatchCreatePartition` so ingestr can register external tables in the AWS Glue Data Catalog that Athena reads from.

### 2. Choose a credential method

Pick **one** of the following based on how you sign in to AWS. If you are unsure which one applies, the quickest test is: open the [AWS console](https://console.aws.amazon.com/), click your name in the top-right corner, and look at the badge under your name — if it says `AssumedRole/AWSReservedSSO_…`, you are signed in via Identity Center (SSO) and should use Option B or C. Otherwise Option A is fine.

#### Option A — IAM user with long-lived access keys (no SSO)

Best for: accounts that are **not** managed by AWS Identity Center, or for creating a dedicated service identity for ingestr.

1. In the [IAM console](https://console.aws.amazon.com/iam/), go to **Users → Create user**, give it a name (e.g. `ingestr-athena`), and continue without granting console access.
2. On the **Set permissions** step, attach a policy granting the S3 / Athena / Glue permissions from step 4 above.
3. Open the new user, go to the **Security credentials** tab → **Access keys** → **Create access key**, choose **Application running outside AWS**, and copy the **Access key ID** (`access_key_id`) and **Secret access key** (`secret_access_key`).

URI:
```plaintext
athena://?bucket=my-company-ingestr&access_key_id=AKIA...&secret_access_key=...&region_name=eu-central-1
```

#### Option B — AWS Identity Center (SSO) via a local profile

Best for: developer machines where you already use `aws configure sso` / `aws sso login`. ingestr reads the credentials (including the rotating session token) from your local AWS credentials file, so you never paste secrets into the URI.

1. If you have not yet, run `aws configure sso` and follow the prompts to attach your Identity Center login to a named profile, e.g. `ingestr`.
2. Whenever the session has expired, run `aws sso login --profile ingestr` to refresh it.
3. In your Identity Center permission set, confirm that the role you assume includes the S3 / Athena / Glue permissions from step 4 above.

URI:
```plaintext
athena://?bucket=my-company-ingestr&profile=ingestr&region_name=eu-central-1
```

#### Option C — Short-lived credentials from the SSO access portal

Best for: a one-off ingestion run from a machine that does not have the AWS CLI configured.

1. Open your AWS access portal (`https://<your-sso-portal>.awsapps.com/start`).
2. Pick the target account, then click **Access keys** next to the permission set you want to use.
3. Copy the **Access key ID**, **Secret access key**, and **Session token** from the screen.

URI:
```plaintext
athena://?bucket=my-company-ingestr&access_key_id=ASIA...&secret_access_key=...&session_token=...&region_name=eu-central-1
```

> [!WARNING]
> Credentials from Option C expire after a few hours. Do not use them for scheduled pipelines — use Option A (dedicated IAM user) or Option B (refreshable SSO profile) instead.

### 3. Run ingestr

Here's a sample command using Option A credentials:
```
ingestr ingest \
  --source-uri "stripe://?api_key=key123" \
  --source-table 'event' \
  --dest-uri "athena://?bucket=bucket_123&access_key_id=access_123&secret_access_key=secret_123&region_name=eu-central-1" \
  --dest-table 'stripe.event'
```
This is a sample command that will copy the data from the Stripe source into Athena.

<img alt="athena_img" src="../media/athena.png" />
