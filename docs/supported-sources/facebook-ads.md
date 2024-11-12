# Facebook Ads

Facebook Ads is the advertising platform that helps users to create targeted ads on Facebook, Instagram and Messenger.

ingestr supports Facebook Ads as a source.

## URI format

The URI format for Facebook Ads is as follows:

```plaintext
facebookads://?access_token=<access_token>&account_id=<account_id>
```

URI parameters:

- `access_token` is associated with Business Facebook App.
- `account_id` is associated with Ad manager.

Both are used for authentication with Facebook Ads API.

The URI is used to connect to Facebook Ads API for extracting data.

## Setting up a Facebook Ads Integration

Facebook Ads requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/facebook_ads#setup-guide).

Once you complete the guide, you should have an access token and an Account ID. Let's say your `access_token` is `abcdef` and `account_id` is `1234`, here's a sample command that will copy the data from Facebook Ads into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.campaigns'
```

The result of this command will be a table in the `facebook.duckdb` database.

## Tables

Facebook Ads source allows ingesting the following sources into separate tables:

- `campaigns`: Retrieves all DEFAULT_CAMPAIGN_FIELDS.
- `ad_sets`: Retrieves all DEFAULT_ADSET_FIELDS.
- `leads`: Retrieves all DEFAULT_LEAD_FIELDS.
- `ads_creatives`: Retrieves all DEFAULT_ADCREATIVE_FIELDS.
- `ads`: Retrieves all DEFAULT_ADS_FIELDS.
- `facebook_insights`: Retrieves all DEFAULT_INSIGHTS_FIELDS.

Use these as `--source-table` parameter in the `ingestr ingest` command.
