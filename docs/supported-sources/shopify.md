# Shopify
[Shopify](https://www.shopify.com/) is a comprehensive e-commerce platform that enables individuals and businesses to create online stores.

ingestr supports Shopify as a source.

## URI Format
The URI format for Shopify is as follows:

```plaintext
shopify://<shopify store URL>?api_key=token
```

URI parameters:
- `shopify store URI`: the URL of the Shopify store you'd like to connect to, e.g. `myawesomestore.myshopify.com`
- `api_key`: the API key used for authentication with the Shopify API

The URI is used to connect to the Shopify API for extracting data. More details on setting up Shopify integrations can be found [here](https://shopify.dev/docs/admin-api/getting-started).

## Setting up a Shopify Integration

Shopify requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/shopify#setup-guide).

Once you complete the guide, you should have an API key and the store name to connect to. Let's say your API key is `shpkey_12345` and the store you'd like to connect to is `my-store`, here's a sample command that will copy the data from the Shopify store into a duckdb database:

```sh
ingestr ingest --source-uri 'shopify://my-store.myshopify.com?api_key=shpkey_12345' --source-table 'orders' --dest-uri duckdb:///shopify.duckdb --dest-table 'shopify.orders'
```

The result of this command will be a table in the `shopify.duckdb` database with JSON columns.

## Available Tables
Shopify source allows ingesting the following sources into separate tables:
- `orders`
- `customers`
- `products`

Use these as `--source-table` parameter in the `ingestr ingest` command.