# Google Ads

> **Warning!**
>
> This source is a Community source and was tested only once. Currently, we **don't test** it on a regular basis.
> If you have any problem with this source, ask for help in our [Slack Community](https://dlthub.com/community).

[Google Ads](https://ads.google.com/home/) is a digital advertising service by Google that allows advertisers
to display ads across Google's search results, websites, and other platforms.

Resources that can be loaded using this verified source are:

| S.No. | Name             | Description                                                             |
|-------|------------------|-------------------------------------------------------------------------|
| 1.    | customers        | Businesses or individuals who pay to advertise their products           |
| 2.    | campaigns        | Structured sets of ad groups and advertisements                         |
| 3.    | change_events    | Modifications made to an account's ads, campaigns, and related settings |
| 4.    | customer_clients | Accounts that are managed by a given account                            |

## Initialize the pipeline
```bash
dlt init google_ads duckdb
```
Here, we chose DuckDB as the destination. Alternatively, you can also choose redshift, bigquery, or any of the other [destinations.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Grab Google Ads credentials
To learn about grabbing the Google Analytics credentials and configuring the verified source, please refer to the [full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_ads#grab-credentials)

## Add credentials

1. Open `.dlt/secrets.toml`. In this file setup the "developer
   token" and "customer ID" as follows:
   ```toml
   [sources.google_ads]
   dev_token = "please set me up!"
   customer_id = "please set me up!"
   impersonated_email = "please set me up"
   ```

   -  `customer_id` in Google Ads is a unique three-part number (formatted as XXX-XXX-XXXX) that identifies
   and helps manage individual Google Ads accounts. It is used for API access and account operations, and
   is visible in the top right corner of your Google Ads dashboard.
   - `impersonated_email` is the email address of the user whose identity the service account will impersonate.

1. Next, for service account authentication:

   ```toml
   [sources.google_analytics.credentials]
   project_id = "project_id" # please set me up!
   client_email = "client_email" # please set me up!
   private_key = "private_key" # please set me up!
   ```

1. Alternatively, for OAuth authentication:

   ```toml
   [sources.google_analytics.credentials]
   client_id = "client_id" # please set me up!
   client_secret = "client_secret" # please set me up!
   refresh_token = "refresh_token" # please set me up!
   project_id = "project_id" # please set me up!
   ```

1. Finally, enter credentials for your chosen destination as per the [docs](https://dlthub.com/docs/dlt-ecosystem/destinations/).

## Run the pipeline

1. Before running the pipeline, ensure that you have installed all the necessary dependencies by
   running the command:
   ```sh
   pip install -r requirements.txt
   ```
1. You're now ready to run the pipeline! To get started, run the following command:
   ```sh
   python google_ads_pipeline.py
   ```
1. Once the pipeline has finished running, you can verify that everything loaded correctly by using
   the following command:
   ```sh
   dlt pipeline <pipeline_name> show
   ```
   For example, the `pipeline_name` for the above pipeline example is
   `dlt_google_ads_pipeline`, you may also use any custom name instead.

💡 To explore additional customizations for this pipeline, we recommend referring to the official `dlt`
Google Ads documentation. It provides comprehensive information and guidance on how to further
customize and tailor the pipeline to suit your specific needs.
You can find the `dlt` Google Ads documentation in the [Setup Guide: Google Ads.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_ads)