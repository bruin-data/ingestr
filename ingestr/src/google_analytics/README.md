# Google Analytics

Google Analytics is a web analytics service that tracks and provides data about user engagement with your website or application. Using this `dlt` Google Analytics verified source and pipeline example, you can load the following resources from Google Analytics to your preferred destination.

| Resource name | Description |
| --- | --- |
| get_metadata | Get all the metrics and dimensions for a report. |
| metrics_table | Loads data for metrics. |
| dimensions_table | Loads data for dimensions. |

To read about authentication for the Google Analytics API, you can refer to our [full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_analytics#google-analytics-api-authentication)

## Initialize the pipeline
```bash
dlt init google_analytics duckdb
```
Here, we chose DuckDB as the destination. Alternatively, you can also choose redshift, bigquery, or any of the other [destinations.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Grab Google Analytics credentials
To learn about grabbing the Google Analytics credentials and configuring the verified source, please refer to the [full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_analytics#google-analytics-api-authentication)

## Add credentials

1. Open `.dlt/secrets.toml`.
2. From the credentials for service account, copy ”project_id”, ”private_key”, and ”client_email” as follows:
    ```toml
    [sources.google_analytics.credentials]
    project_id = "set me up" # GCP Source project ID!
    private_key = "set me up" # Unique private key !(Must be copied fully including BEGIN and END PRIVATE KEY)
    client_email = "set me up" # Email for source service account
    location = "set me up" #Project Location For ex. “US”
    ```

3. Enter the credentials for your chosen destination as per the [documentation.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Run the pipeline

1. Install the requirements by using the following command:
    ```bash
    pip install -r requirements.txt
    ```

2. Run the pipeline by using the following command:
    ```bash
    python google_analytics_pipelines.py
    ```

3. Make sure that everything is loaded as expected by using the command:
    ```bash
    dlt pipeline <pipeline_name> show
    ```

    For example, the pipeline_name for the above pipeline example is `dlt_google_analytics_pipeline`, but you may also use any custom name instead.


💡 To explore additional customizations for this pipeline, we recommend referring to the official `dlt`
Google Analytics documentation. It provides comprehensive information and guidance on how to further
customize and tailor the pipeline to suit your specific needs.
You can find the `dlt` Google Analytics documentation in the [Setup Guide: Google Analytics.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_analytics)
