# Applovin Max
[AppLovin Max](https://www.applovin.com/max) is a specific tool from AppLovin that helps app developers earn more money from ads by automatically picking the best-paying ads from different ad networks.

`ingestr` allows ingesting data from AppLovin Max reporting API.

## URI Format

The URI format for Applovin Max is as follows:
```
applovinmax://?api_key=<your_api_key>&application=<application_name>
```

URI Parameters:
- `api_key`: report key generated from your [applovin account](https://www.applovin.com/analytics#keys).
- `application`: The name of the application to ingest data from.

## Setting up Applovin Integration

### Generate a Report Key
You can generate a report key from your [analytics dashboard](https://www.applovin.com/analytics#keys).

### Example:
Retrieve data for application with `application_name` com.example.app:
```sh
ingestr ingest \
    --source-uri "applovinmax://?api_key=key_123&application=com.example.app" \
    --source-table "ad_revenue" \
    --dest-uri "duckdb:///applovin_max.db"  \
    --dest-table "dest.ad_revenue" 
```
This command will retrieve data and save it to the destination table in the DuckDB database.

## Table
| `ad_revenue`: Provides daily metrics from the `ad_revenue`


