# Adjust

[Adjust](https://www.adjust.com/) is a mobile marketing analytics platform that provides solutions for measuring and optimizing campaigns, as well as protecting user data.

ingestr supports Adjust as a source.

## URI Format

The URI format for Adjust is as follows:

```plaintext
adjust://?api_key=<api-key-here>
```

An API token is required to retrieve reports from the Adjust reporting API. please follow the guide to [obtain a API key](https://dev.adjust.com/en/api/rs-api/authentication/).

Once you complete the guide, you should have an API key. Let's say your API key is `nr_123`, here's a sample command that will copy the data from Adjust into a duckdb database:

```sh
ingestr ingest --source-uri 'adjust://?api_key=nr_123' --source-table 'campaigns' --dest-uri duckdb:///adjust.duckdb --dest-table 'adjust.output' --interval-start '2024-09-05' --interval-end '2024-09-08'
```

The result of this command will be a table in the `adjust.duckdb` database

Available Source Table:
Adjust source allows ingesting the following source into separate tables:

-`Campaigns`: Retrieves data for a campaign, showing the app's revenue and network costs over multiple days.

--`Creatives`: Retrieves data for a creative assest, detailing the app's revenue and network costs across multiple days
