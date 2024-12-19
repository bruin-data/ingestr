# DynamoDB

Amazon [DynamoDB](https://aws.amazon.com/dynamodb/) is a managed NoSQL database service provided by Amazon Web Services (AWS). It supports key-value and document data structures and is designed to handle a wide range of applications requiring scalability and performance. 

## URI format

The URI format for DynamoDB is as follows:
```plaintext
dynamodb://dynamodb.<region>.amazonaws.com?access_key_id=<aws_access_key_id>&secret_access_key=<aws_secret_access_key>
```

URI parameters:

- `access_key_id`: Identifes an IAM account. 
- `secret_access_key`: Password for the IAM account.


## Setting up a DynamoDB integration

### Prerequisites
* AWS IAM access key pair.
* A DynamoDB Table that you will to load data from

To obtain the access keys, use the IAM console on AWS. See [IAM Documentation](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_access-keys.html) for more information.


### Configuring Permissions
To use DynamoDB source, the user account must have the following IAM permissions:
* `dynamodb:DescribeTable`
* `dynamodb:Scan`

Following AWS Best practices, you can create an IAM policy that you can assign to the user account you wish to use with `ingestr`.
Below is a sample policy:
```json
{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Sid": "Statement1",
			"Effect": "Allow",
			"Action": [
				"dynamodb:DescribeTable",
				"dynamodb:Scan"
			],
			"Resource": [
				"<TABLE_ARN>"
			]
		}
	]
}
```

Replace `TABLE_ARN` with the DynamoDB [Amazon Resource Name](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html) of your Table. You can find the ARN for your table in the [DynamoDB console](https://console.aws.amazon.com/dynamodb/home). You can add as many tables you want in the `Resource` field. Alternatively, if you'd like to give access to all tables that you own, you can set `Resource` to `["*"]`.

### Example: Simple load

For this example, we'll assume the value of `access_key_id` and `secret_access_key` are `user` and `pass` respectively.

Say you have a table called `absolute-armadillo` in the region `ap-south-1` and you want to load this data to a duckdb database called `animal.db`.

You run the following to achieve this:
```sh
ingestr ingest \
    --source-uri "dynamodb://dynamodb.ap-south-1.amazonaws.com?access_key_id=user&secret_access_key=pass" \
    --source-table "absolute-armadillo" \
    --dest-uri "duckdb://./animal.db" \
    --dest-table "public.armadillo"
```

### Example: Incremental load
`ingestr` supports incremental loading. Incremental loading is a technique whereby only rows or fields that are changed are fetched. This reduces load times of subsequent runs and improves efficiency of your pipelines.

Assuming the same setup from [Simple Load](#example-simple-load), we can run incremental load with:
```sh
ingestr ingest \
    --source-uri "dynamodb://dynamodb.ap-south-1.amazonaws.com?access_key_id=user&secret_access_key=pass" \
    --source-table "absolute-armadillo" \
    --dest-uri "duckdb://./animal.db" \
    --dest-table "public.armadillo" \
    --incremental-strategy "replace" \
    --incremental-key "updated_at"
```

Assuming that `absolute-armadillo` table has a datetime field called `updated_at`, whenever you run this command, only rows with value greater than `MAX(updated_at)` from previous load will be fetched from DynamoDB.

> [!WARNING]
> DynamoDB doesn't support indexed range scans.
> Whenever you run `ingestr ingest`, the whole table is scanned.
> Although `ingestr` does specify a filter critiera, DynamoDB only applies
> this _after_ running the table scan.