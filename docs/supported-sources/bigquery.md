# Google BigQuery
BigQuery is a fully managed, serverless data warehouse that enables scalable analysis over petabytes of data.

ingestr supports BigQuery as both a source and destination.

## URI format
The URI format for BigQuery is as follows:

```plaintext
bigquery://<project-name>?credentials_path=/path/to/service/account.json&location=<location>
```

URI parameters:
- `project-name`: the name of the project in which the dataset resides
- `credentials_path`: the path to the service account JSON file
- `location`: optional, the location of the dataset

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's BigQuery dialect [here](https://github.com/googleapis/python-bigquery-sqlalchemy?tab=readme-ov-file#connection-string-parameters).

### Using GCS as a staging area

ingestr can use GCS as a staging area for BigQuery. To do this, you need to set the `--staging-bucket` flag when you are running the command.

```bash
ingestr ingest 
    --source-uri $SOURCE_URI
    --dest-uri $BIGQUERY_URI
    --source-table raw.input 
    --dest-table raw.output
    --staging-bucket "gs://your-bucket-name" # [!code focus]
```

