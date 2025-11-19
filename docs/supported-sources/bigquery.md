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
- `credentials_path`: the path to the service account JSON file (required unless using ADC)
- `credentials_base64`: base64-encoded service account JSON (required unless using ADC)
- `use_adc`: set to `true` to use Application Default Credentials (optional)
- `location`: optional, the location of the dataset

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's BigQuery dialect [here](https://github.com/googleapis/python-bigquery-sqlalchemy?tab=readme-ov-file#connection-string-parameters).

### Using Application Default Credentials (ADC)

ingestr supports Google Cloud Application Default Credentials (ADC), which allows you to authenticate using `gcloud auth application-default login` instead of providing a service account key file.

To use ADC, explicitly enable it by setting `use_adc=true` in the URI:
```plaintext
bigquery://<project-name>?use_adc=true&location=<location>
```

Before using ADC, you need to authenticate with gcloud:
```bash
gcloud auth application-default login
```

This command creates credentials in a well-known location that ingestr will automatically discover:
- **Linux/macOS**: `$HOME/.config/gcloud/application_default_credentials.json`
- **Windows**: `%APPDATA%\gcloud\application_default_credentials.json`

ADC will also check the `GOOGLE_APPLICATION_CREDENTIALS` environment variable if set, and will use the attached service account when running on Google Cloud resources.

For more information about Application Default Credentials, see the [Google Cloud documentation](https://cloud.google.com/docs/authentication/application-default-credentials).

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

