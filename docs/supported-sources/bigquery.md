# Google BigQuery
BigQuery is a fully-managed, serverless data warehouse that enables scalable analysis over petabytes of data.

ingestr supports BigQuery as both a source and destination.

## URI Format
The URI format for BigQuery is as follows:

```plaintext
bigquery://<project-name>?credentials_path=/path/to/service/account.json&location=<location>
```

URI parameters:
- `project-name`: the name of the project in which the dataset resides
- `credentials_path`: the path to the service account JSON file
- `location`: the location of the dataset (optional)

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's BigQuery dialect [here](https://github.com/googleapis/python-bigquery-sqlalchemy?tab=readme-ov-file#connection-string-parameters).


