# Google Analytics
[Google Analytics](https://marketingplatform.google.com/about/analytics/) is a service for web analytics that tracks and provides data regarding user engagement with your website or application.

ingestr supports Google Analytics as a source.

## URI format
The URI format for Google Analytics is as follows:

```plaintext
googleanalytics://?credentials_path=/path/to/service/account.json&property_id=<property_id>
```

URI parameters:
- `credentials_path`: The path to the service account JSON file.
- `property_id`: It is a unique number that identifies a particular property on Google Analytics. [Follow this guide](https://developers.google.com/analytics/devguides/reporting/data/v1/property-id#what_is_my_property_id) if you don't know your property ID.

## Setting up an Google Analytics Integration
Google Analytics requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_analytics#grab-google-service-account-credentials). Once you complete the guide, you should have an `.json` file  and `project_id`. 

## Table: Custom Reports
Custom reports allow you to retrieve data based on specific `dimensions` and  `metrics`.

Custom Table Format:
```
custom:<dimensions>:<metrics>
```

### Parameters:
- `dimensions`(required): A comma-separated list of [dimensions](https://developers.google.com/analytics/devguides/reporting/data/v1/exploration-api-schema#dimensions) to retrieve.
- `metrics`(required): A comma-separated list of [metrics](https://developers.google.com/analytics/devguides/reporting/data/v1/exploration-api-schema#metrics) to retrieve.

### Example

```sh
ingestr ingest \
    --source-uri "googleanalytics://?credentials_path="ingestr/src/g_analytics.json&property_id=id123" \
    --source-table "custom:city,date:clicks,activeUsers,newUsers" \
    --dest-uri "duckdb:///analytics.duckdb" \
    --dest-table "dest.custom"
```

This command will retrieve report and save it to the `dest.custom` table in the DuckDB database.

<img alt="google_analytics_img" src="../media/googleanalytics.png" />