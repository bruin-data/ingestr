# Google Cloud Storage

[Google Cloud Storage](https://cloud.google.com/storage?hl=en) is an online file storage web service for storing and accessing data on Google Cloud Platform infrastructure. The service combines the performance and scalability of Google's cloud with advanced security and sharing capabilities. It is an Infrastructure as a Service (IaaS), comparable to Amazon S3. 

## URI format

The URI format for Google Cloud Storage is as follows:

```plaintext
gs://?credentials_path=/path/to/service-account.json>
```

URI parameters:

- `credentials_path`: path to file containing your Google Cloud [Service Account](https://cloud.google.com/iam/docs/service-account-overview)

The `--source-table` must be in the format:
```
{bucket name}/{file glob}
```

## Setting up a GCS Integration

To use Google Cloud Storage source in `ingestr`, you will need:
* A Google Cloud Project.
* A Service Account with atleast [roles/storage.objectUser](https://cloud.google.com/storage/docs/access-control/iam-roles) IAM permission.
* A Service Account key file for the corresponding service account.

For more information on how to create a Service Account or it's keys, see [Create service accounts](https://cloud.google.com/iam/docs/service-accounts-create) and [Create or delete service account keys](https://cloud.google.com/iam/docs/keys-create-delete) on Google Cloud docs.

## Example

Let's assume that:
* Service account key in available in the current directory, under the filename `service_account.json`. 
* The bucket you want to load data from is called `my-org-bucket`
* The source file is available at `data/latest/dump.csv`
* The data needs to be saved in a DuckDB database called `local.db`
* The destination table name will be `public.latest_dump`

You can run the following command line to achieve this:

```sh
ingestr ingest \
    --source-uri "gs://?credentials_path=$PWD/service_account.json" \
    --source-table "my-org-bucket/data/latest/dump.csv" \
    --dest-uri "duckdb:///local.db" \
    --dest-table "public.latest_dump"
```

## Supported File Formats
`gs` source only supports loading files in the following formats:
* `csv`: Comma Separated Values (supports Tab Separated Values as well)
* `parquet`: [Apache Parquet](https://parquet.apache.org/) storage format.
* `jsonl`: Line delimited JSON. see [https://jsonlines.org/](https://jsonlines.org/)

## File Pattern
`ingestr` supports [glob](https://en.wikipedia.org/wiki/Glob_(programming)) like pattern matching for `gs` source.
This allows for a powerful pattern matching mechanism that allows you to specify multiple files in a single `--source-table`.

Below are some examples of path patterns, each path pattern is glob you can specify after the bucket name:

- `**/*.csv`: Retrieves all the CSV files, regardless of how deep they are within the folder structure.
- `*.csv`: Retrieves all the CSV files from the first level of a folder.
- `myFolder/**/*.jsonl`: Retrieves all the JSONL files from anywhere under `myFolder`.
- `myFolder/mySubFolder/users.parquet`: Retrieves the `users.parquet` file from `mySubFolder`.
- `employees.jsonl`: Retrieves the `employees.jsonl` file from the root level of the bucket.
