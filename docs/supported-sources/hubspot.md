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

| Table | PK | Inc Key | Inc Strategy | Details |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [companies](https://developers.hubspot.com/docs/reference/api/crm/objects/companies#get-%2Fcrm%2Fv3%2Fobjects%2Fcompanies) | hs_object_id | hs_lastmodifieddate | merge | Retrieves information about organizations. |
| [contacts](https://developers.hubspot.com/docs/reference/api/crm/objects/contacts#get-%2Fcrm%2Fv3%2Fobjects%2Fcontacts) | hs_object_id | lastmodifieddate | merge | Retrieves information about visitors, potential customers, and leads. |
| [deals](https://developers.hubspot.com/docs/reference/api/crm/objects/deals#get-%2Fcrm%2Fv3%2Fobjects%2Fdeals) | hs_object_id | hs_lastmodifieddate | merge | Retrieves deal records and tracks deal progress. |
| [tickets](https://developers.hubspot.com/docs/api-reference/crm-tickets-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Handles requests for help from customers or users. |
| [products](https://developers.hubspot.com/docs/api-reference/crm-products-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves pricing information of products. |
| [quotes](https://developers.hubspot.com/docs/reference/api/crm/objects/quotes#get-%2Fcrm%2Fv3%2Fobjects%2Fquotes) | hs_object_id | hs_lastmodifieddate | merge | Retrieves price proposals that salespeople can create and send to their contacts. |
| [calls](https://developers.hubspot.com/docs/api-reference/crm-calls-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves call engagement records. |
| [emails](https://developers.hubspot.com/docs/api-reference/crm-emails-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves email engagement records. |
| [feedback_submissions](https://developers.hubspot.com/docs/reference/api/crm/objects/feedback-submissions) | hs_object_id | hs_lastmodifieddate | merge | Retrieves customer feedback survey responses. |
| [line_items](https://developers.hubspot.com/docs/api-reference/crm-line-items-v3/guide#crm-api-line-items) | hs_object_id | hs_lastmodifieddate | merge | Retrieves individual products or services associated with deals. |
| [meetings](https://developers.hubspot.com/docs/api-reference/crm-meetings-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves meeting engagement records. |
| [notes](https://developers.hubspot.com/docs/api-reference/crm-notes-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves note engagement records. |
| [tasks](https://developers.hubspot.com/docs/api-reference/crm-tasks-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves task engagement records. |
| [carts](https://developers.hubspot.com/docs/reference/api/crm/objects/carts) | hs_object_id | hs_lastmodifieddate | merge | Retrieves shopping cart records. |
| [discounts](https://developers.hubspot.com/docs/reference/api/crm/objects/discounts) | hs_object_id | hs_lastmodifieddate | merge | Retrieves discount records. |
| [fees](https://developers.hubspot.com/docs/api-reference/crm-fees-v3/guide#crm-api-fees) | hs_object_id | hs_lastmodifieddate | merge | Retrieves fee records. |
| [invoices](https://developers.hubspot.com/docs/api-reference/crm-invoices-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves invoice records. |
| [commerce_payments](https://developers.hubspot.com/docs/api-reference/crm-commerce-payments-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves commerce payment records. |
| [taxes](https://developers.hubspot.com/docs/api-reference/crm-taxes-v3/guide) | hs_object_id | hs_lastmodifieddate | merge | Retrieves tax records. |
| [owners](https://developers.hubspot.com/docs/api-reference/crm-crm-owners-v3/guide#crm-api-owners) | id | – | merge | Retrieves HubSpot users who can be assigned to CRM records. |
| [schemas](https://developers.hubspot.com/docs/reference/api/crm/objects/schemas#get-%2Fcrm-object-schemas%2Fv3%2Fschemas) | id | – | merge | Returns all object schemas that have been defined for your account. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Property History Tables

For every CRM object table listed above, a corresponding **property history** table is available. These tables return one row per property change, enabling you to track how properties changed over time.

Use the format `property_history:<table>` as the `--source-table` value:

| Table | Details |
| ----------------------------------- | --------------------------------------------------------- |
| property_history:contacts           | Property change history for contacts.                     |
| property_history:companies          | Property change history for companies.                    |
| property_history:deals              | Property change history for deals.                        |
| property_history:tickets            | Property change history for tickets.                      |
| property_history:products           | Property change history for products.                     |
| property_history:quotes             | Property change history for quotes.                       |
| property_history:calls              | Property change history for calls.                        |
| property_history:emails             | Property change history for emails.                       |
| property_history:feedback_submissions | Property change history for feedback submissions.       |
| property_history:line_items         | Property change history for line items.                   |
| property_history:meetings           | Property change history for meetings.                     |
| property_history:notes              | Property change history for notes.                        |
| property_history:tasks              | Property change history for tasks.                        |
| property_history:carts              | Property change history for carts.                        |
| property_history:discounts          | Property change history for discounts.                    |
| property_history:fees               | Property change history for fees.                         |
| property_history:invoices           | Property change history for invoices.                     |
| property_history:commerce_payments  | Property change history for commerce payments.            |
| property_history:taxes              | Property change history for taxes.                        |

> **Note:** The `owners` and `schemas` tables do not have history variants.

Custom objects also support history via `property_history:custom:<objectType>` (e.g., `property_history:custom:myObject`).

## Incremental Loading

HubSpot supports incremental loading out of the box. On the first run, ingestr performs a full load of all records. On subsequent runs, it uses the `hs_lastmodifieddate` field to fetch only records that have been created or updated since the last successful run.

No additional flags are needed — incremental state is managed automatically by ingestr.

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
