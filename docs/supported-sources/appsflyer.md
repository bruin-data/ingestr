# AppsFlyer

[AppsFlyer](https://www.appsflyer.com/) is a mobile marketing analytics and attribution platform that helps businesses track, measure, and optimize their app marketing efforts across various channels.

ingestr supports AppsFlyer as a source.

The URI format for AppsFlyer is as follows:

```plaintext
appsflyer://?api_key=<api-key>
```

An API token is required to retrieve reports from the AppsFlyer API. Please follow the guide to [obtain a API key](https://support.appsflyer.com/hc/en-us/articles/360004562377-Managing-AppsFlyer-tokens)

Once you complete the guide, you should have an API key. Let's say your API key is `ey123`, here's a sample command that will copy the data from AppsFlyer into a DuckDB database:

```bash
ingestr ingest 
    --source-uri 'appsflyer://?api_key=ey123' 
    --source-table 'campaigns' 
    --dest-uri duckdb:///appsflyer.duckdb 
    --dest-table 'appsflyer.output'
```

The result of this command will be a table in the `appsflyer.duckdb` database.

## Tables
AppsFlyer source allows ingesting the following sources into separate tables:

- `campaigns`: Retrieves data for campaigns, detailing the app's costs, loyal users, total installs, and revenue over multiple days.
- `creatives`: Retrieves data for a creative asset, including revenue and cost.

Use these as `--source-table` parameter in the `ingestr ingest` command.
