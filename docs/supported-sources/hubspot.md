# HubSpot

[HubSpot](https://www.hubspot.com/) is a customer relationship management software that helps businesses attract visitors, connect with customers, and close deals.

ingestr supports HubSpot as a source.

## URI format

The URI format for HubSpot is as follows:

```plaintext
hubspot://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: The API key is used for authentication with the HubSpot API.

The URI is used to connect to the HubSpot API for extracting data.

## Setting up a HubSpot Integration

HubSpot requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/hubspot#setup-guide).

Once you complete the guide, you should have an API key. Let's say your API key is `pat_test_12345`, here's a sample command that will copy the data from HubSpot into a DuckDB database:

```sh
ingestr ingest --source-uri 'hubspot://?api_key=pat_test_12345' --source-table 'companies' --dest-uri duckdb:///hubspot.duckdb --dest-table 'companies.data'
```

The result of this command will be a table in the `hubspot.duckdb` database.

## Tables

HubSpot source allows ingesting the following sources into separate tables:

- `companies`: Retrieves information about organizations.
- `deals`: Retrieves deal records and tracks deal progress.
- `products`: Retrieves pricing information of products.
- `tickets`: Handles requests for help from customers or users.
- `quotes`: Retrieves price proposals that salespeople can create and send to their contacts.
- `contacts`: Retrieves information about visitors, potential customers, and leads.

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Custom Objects

HubSpot custom objects are supported by using the `custom` table.

The format for the custom object is as follows:

```plaintext
custom:<custom_object_name>:<optional associations>
```

For example, to ingest the `course` custom object with the `contacts` and `companies` associations, the format would be:

```plaintext
custom:course:contacts,companies
```

This would pull all the data for the `course` custom object and include the `contacts` and `companies` associations in the resulting table.


