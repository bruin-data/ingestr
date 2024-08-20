# Facebook Ads

Facebook Ads is the advertising platform that helps users to create targeted ads on Facebook, Instagram and Messenger.

ingestr supports Facebook Ads as a source.

## URI Format

The URI format for Facebook Ads is as follows:

```plaintext
facebookads://?access_token=<access_token>&account_id=<account_id>
```

URI parameters:

- `access_token`
- `account_id`

The URI is used to connect to the Facebook Ads API for extracting data.

## Setting up a Facebook Ads Integration

Facebook Ads requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/facebook_ads#setup-guide).

Once you complete the guide, you should have an Access_Token and Account ID . Let's say your access_token is `abcdef` and account_id is `1234` , here's a sample command that will copy the data from Facebook Ads into a duckdb database:

```sh
ingestr ingest --source-uri 'facebookads://?access_token=easdyh&account_id=1234' --source-table 'campaigns' --dest-uri duckdb:///facebook.duckdb --dest-table 'facebook.campaigns'
```

The result of this command will be a table in the `facebook.duckdb` database.

## Available Tables

Facebook Ads source allows ingesting the following sources into separate tables:

- `campaigns`
- `ad_sets`
- `ads`
- `ad_leads`
- `creatives`
- `facebook_insights`

Use these as `--source-table` parameter in the `ingestr ingest` command.
