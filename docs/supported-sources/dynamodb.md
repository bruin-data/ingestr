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

### Example

For this example, we'll assume the value of `access_key_id` and `secret_access_key` are `user` and `pass` respectively.

Say you have a table called `absolute-armadillo` in the region `ap-south-1` and you want to load this data to a duckdb database called `animal.db`.

You run the following to achieve this:
```sh
ingestr ingest \
    --source-uri "dynamodb://dynamodb.ap-south-1.amazonaws.com?access_key_id=user&secret_access_key=pass" \
    --source-table "absolute-armadillo" \
    --dest-uri "duckdb://./animal.db"
    --dest-table "public.armadillo"
```

