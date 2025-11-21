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
 
Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [companies](https://developers.hubspot.com/docs/reference/api/crm/objects/companies#get-%2Fcrm%2Fv3%2Fobjects%2Fcompanies)     | - | –                | replace               | Retrieves information about organizations. |
| [contacts](https://developers.hubspot.com/docs/reference/api/crm/objects/contacts#get-%2Fcrm%2Fv3%2Fobjects%2Fcontacts)     | - | –                | replace               | Retrieves information about visitors, potential customers, and leads. |
| [deals](https://developers.hubspot.com/docs/reference/api/crm/objects/deals#get-%2Fcrm%2Fv3%2Fobjects%2Fdeals)       | - | –                | replace               | Retrieves deal records and tracks deal progress.|
| [tickets](https://developers.hubspot.com/docs/reference/api/crm/objects/tickets#basic)     | - | –                | replace               | Handles requests for help from customers or users. |
| [products](https://developers.hubspot.com/docs/reference/api/crm/objects/products#get-%2Fcrm%2Fv3%2Fobjects%2Fproducts)     | - | –                | replace               | Retrieves pricing information of products. |
| [quotes](https://developers.hubspot.com/docs/reference/api/crm/objects/quotes#get-%2Fcrm%2Fv3%2Fobjects%2Fquotes)        | - | –                | replace               | Retrieves price proposals that salespeople can create and send to their contacts. |
| [schemas](https://developers.hubspot.com/docs/reference/api/crm/objects/schemas#get-%2Fcrm-object-schemas%2Fv3%2Fschemas)      | id | –                | merge               | Returns all object schemas that have been defined for your account.  |

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Custom Objects

HubSpot allows you to create custom objects to store unique business data that's not covered by the standard objects. ingestr supports ingesting data from custom objects using the following format:

```plaintext
custom:<custom_object_name>
```

or with associations to other objects:

```plaintext
custom:<custom_object_name>:<associations>
```

### Parameters

- `custom_object_name`: The name of your custom object in HubSpot (can be either singular or plural form)
- `associations` (optional): Comma-separated list of object types to include as associations (e.g., `companies,deals,tickets,contacts`)

### Examples

Ingesting a custom object called "licenses":

```sh
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'custom:licenses' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'licenses.data'
```

Ingesting a custom object with associations to companies, deals, and contacts:

```sh
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'custom:licenses:companies,deals,contacts' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'licenses.data'
```

When you include associations, the response will contain information about the related objects, allowing you to track relationships between your custom objects and standard HubSpot objects.

> [!WARNING]
> Hubspot does not support incremental loading, which means ingestr will do a full-refresh.


