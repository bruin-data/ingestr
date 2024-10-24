# AppsFlyer

[AppsFlyer](https://www.appsflyer.com/) is a mobile marketing analytics and attribution platform that helps businesses track, measure, and optimize their app marketing efforts across various channels.

ingestr supports AppsFlyer as a source.

The URI format for AppsFlyer is as follows:

```plaintext
appsflyer://?api_key=<api-key>
```

An API token is required to retrieve reports from the AppsFlyer API. Please follow the guide to [obtain a API key](https://support.appsflyer.com/hc/en-us/articles/360004562377-Managing-AppsFlyer-tokens)

Once you complete the guide, you should have an API key. Let's say your API key is ey123, here's a sample command that will copy the data from AppsFlyer into a duckdb database:

ingestr ingest --source-uri 'appsflyer://?api_key=ey123' --source-table 'cacreativesmpaigns' --dest-uri duckdb:///appsflyer.duckdb --dest-table 'dest.creatives' --interval-start '2024-08-01' --interval-end '2024-08-28'

The result of this command will be a table in the appsflyer.duckdb database

The AppsFlyer source allows ingesting the "Creatives" data into a table:

- Creatives: Retrieves data on creative assets, including revenue and cost.

Use creatives as the `--source-table` parameter in the `ingestr ingest` command.
