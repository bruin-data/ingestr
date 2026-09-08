# HubSpot

[HubSpot](https://www.hubspot.com/) is a customer relationship management software that helps businesses attract visitors, connect with customers, and close deals.

ingestr supports HubSpot as a source.

## URI format

The URI format for HubSpot is as follows:

```plaintext
hubspot://?api_key=<api-key-here>
```

or, using a service key:

```plaintext
hubspot://?service_key=<service-key-here>
```

URI parameters:

- `api_key`: A private app access token used for authentication with the HubSpot API.
- `service_key`: A HubSpot [service key](https://developers.hubspot.com/blog/hubspot-service-keys-the-right-api-credential-for-data-integrations) used for authentication with the HubSpot API.

Provide exactly one of `api_key` or `service_key`. Both are sent to HubSpot as an HTTP Bearer token, so they are interchangeable as the credential.

> **Note:** HubSpot service keys are currently in **public beta**. The feature may change before general availability, so keep that in mind before relying on it for production workloads.

The URI is used to connect to the HubSpot API for extracting data.

## Setting up a HubSpot Integration

HubSpot supports two credential types. Pick whichever fits your account; ingestr accepts either.

### Option A: Service Key (public beta)

Go to **Settings → Integrations → Service Keys** in your HubSpot account, create a key, grant it the required scopes, and copy the generated key. Use it via `service_key=` in the URI.

### Option B: Private App access token

To connect to HubSpot with a private app access token, create a Legacy Private App and copy its token.

#### Step 1: Create a Legacy App

1. Log in to your [HubSpot account](https://app.hubspot.com/)
2. Click the **Settings** icon (gear) in the top navigation
3. In the left sidebar, navigate to **Integrations** → **Private Apps**
4. Click **Create Legacy App**
5. Select **Private** as the app type
6. If prompted to create a service key, select **I still want a legacy private app** and continue

#### Step 2: Configure the App

1. Enter a name for your app (e.g., "Data Integration")
2. Optionally add a description
3. Skip the webhook setup (not needed for data ingestion)
4. Click the **Scopes** tab

#### Step 3: Select Scopes

Add the scopes for the data you want to access. Common scopes include:
- **CRM**: `crm.objects.contacts.read`, `crm.objects.companies.read`, `crm.objects.deals.read`
- **Tickets**: `tickets`
- **Sales**: `sales-email-read`
- **Forms**: `forms`

Select **Read** access for each object type you want to ingest.

#### Step 4: Create and Get the Token

1. Click **Create app** in the top right
2. Review the information and click **Continue creating**
3. Copy the **Access token** that is displayed (starts with `pat-`)
4. Store this token securely

> **Note**: The token is only shown once. If you lose it, you'll need to rotate the token in the app settings.

Once you have a credential (for example `pat_test_12345`), here's a sample command that will copy the data from HubSpot into a DuckDB database:

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
| [pipelines](https://developers.hubspot.com/docs/reference/api/crm/pipelines) | object_type, pipeline_id | – | replace | Pipeline definitions across all pipelined object types. One row per pipeline. |
| [pipeline_stages](https://developers.hubspot.com/docs/reference/api/crm/pipelines) | object_type, pipeline_id, stage_id | – | replace | Stage definitions for each pipeline. One row per stage.|

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Property History Tables

For every CRM object table listed above, a corresponding **property history** table is available. These tables return one row per property change, enabling you to track how properties changed over time.

Use the format `property_history:<table>` as the `--source-table` value. An optional comma-separated list of property names can be appended (`property_history:<table>:<prop1>,<prop2>,...`) to restrict the history to just those properties — see [Filtering properties in property_history:* tables](#filtering-properties-in-property-history-tables).

| Table | PK | Inc Key | Inc Strategy | Details |
| ------------------------------------- | ---------------------------------------- | --------- | ------------ | ------------------------------------------------------- |
| property_history:contacts             | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for contacts.                   |
| property_history:companies            | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for companies.                  |
| property_history:deals                | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for deals.                      |
| property_history:tickets              | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for tickets.                    |
| property_history:products             | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for products.                   |
| property_history:quotes               | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for quotes.                     |
| property_history:calls                | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for calls.                      |
| property_history:emails               | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for emails.                     |
| property_history:feedback_submissions | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for feedback submissions.       |
| property_history:line_items           | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for line items.                 |
| property_history:meetings             | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for meetings.                   |
| property_history:notes                | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for notes.                      |
| property_history:tasks                | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for tasks.                      |
| property_history:carts                | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for carts.                      |
| property_history:discounts            | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for discounts.                  |
| property_history:fees                 | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for fees.                       |
| property_history:invoices             | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for invoices.                   |
| property_history:commerce_payments    | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for commerce payments.          |
| property_history:taxes                | hs_object_id, property_name, timestamp   | timestamp | merge        | Property change history for taxes.                      |


> **Note:** The `owners` and `schemas` tables do not have history variants.

Custom objects also support history via `property_history:custom:<objectType>` (e.g., `property_history:custom:myObject`).

### Filtering properties in property_history:* tables

By default, `property_history:*` tables return change history for **every** property on the object type. For tenants with many properties this produces large, slow responses and consumes more HubSpot API quota than necessary.

You can append a comma-separated allow-list of property names to the table name to restrict the request to just those properties. When the suffix is present, ingestr passes exactly that list to HubSpot's API, so only those properties' history is returned. When the suffix is omitted, behavior is unchanged.

Syntax:

- Built-in object: `property_history:<object>[:<prop1>,<prop2>,...]`
- Custom object:   `property_history:custom:<object>[:<prop1>,<prop2>,...]`

Examples:

Fetch only `email`, `firstname`, and `lastname` history for contacts:

```sh
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'property_history:contacts:email,firstname,lastname' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'contacts.property_history'
```

Fetch only `amount` and `dealstage` history for deals:

```sh
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'property_history:deals:amount,dealstage' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'deals.property_history'
```

Fetch only `field_a` and `field_b` history for a custom object `my_object`:

```sh
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'property_history:custom:my_object:field_a,field_b' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'my_object.property_history'
```

## Overriding associations

Each built-in table fetches a default set of associations. You can override that list by appending `:<assoc1>,<assoc2>` to the table name. The suffix **replaces** the default list, so you can narrow it down to just what you need, or include custom object names. Use `<table>:` (colon with empty list) to skip associations entirely.

```sh
# only fetch companies and deals for contacts
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'contacts:companies,deals' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'contacts.data'

# fetch contacts with no associations 
ingestr ingest \
  --source-uri 'hubspot://?api_key=pat_test_12345' \
  --source-table 'contacts:' \
  --dest-uri duckdb:///hubspot.duckdb \
  --dest-table 'contacts.data'
```

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

## HubSpot as a destination

ingestr can also write **into** HubSpot. Use the same URI as the source, with a private app token that has **write** scopes for the objects you're loading:

```plaintext
hubspot://?api_key=<token>
```

or, using a service key:

```plaintext
hubspot://?service_key=<token>
```

Each source column becomes a HubSpot property of the same name — name your columns to match the target properties. Null values are skipped (they won't overwrite existing data). The `--dest-table` is any writable object: standard (`contacts`, `deals`, …), engagement/commerce (`notes`, `invoices`, …), or a custom object (`p12345_car` or `2-12345678`).

### Operations

The `id_property` param decides what happens to each row:

| `id_property` | operation | behavior |
| --- | --- | --- |
| _(unset)_ | create | every row is a new record |
| a unique property, e.g. `email` | upsert | update if it exists, else create |
| `hs_object_id` | update | update by record id; rows with no id are skipped (warned), ids not found in HubSpot are rejected |

Create and update work for any object. **Upsert** needs a writable unique property, which only some objects have:

| object | upsert key |
| --- | --- |
| contacts | `email` |
| products | `hs_sku` |
| line items | `hs_external_id` |
| tickets | `hs_external_object_ids` |
| commerce_payments | `hs_external_reference_id` |
| companies, deals, quotes | none — use `hs_object_id` (update) or a custom unique property |

### Parameters

Append to `--dest-table` as a query string (e.g. `contacts?id_property=email`):

| param | purpose |
| --- | --- |
| `id_property` | property to match on (see table above); omit to always create |
| `id_column` | source column supplying the id value, if named differently; defaults to `id_property` |
| `on_error` | `fail` (default) aborts on rejects; `skip` logs bad rows and continues |

### Associations

Use the `associations` dest-table to link two existing records (one link per source row):

```
associations?from=contacts&to=companies&from_id_column=contact_id&to_id_column=company_id
```

| param | purpose |
| --- | --- |
| `from` / `to` | the two object types to link |
| `from_id_column` / `to_id_column` | source columns holding each record's id |
| `association_type` | optional numeric type id for a labeled association (default is unlabeled) |
| `association_category` | optional; `HUBSPOT_DEFINED` (default) or `USER_DEFINED`, used with `association_type` |

Both records must already exist — load them first, then run the `associations` ingestion with a source table of id pairs.

### Examples

```sh
# Upsert contacts on email
ingestr ingest --source-uri '<src>' --source-table 'public.contacts' \
  --dest-uri 'hubspot://?api_key=pat_test_12345' \
  --dest-table 'contacts?id_property=email'

# Update companies by record id, skipping rejects
ingestr ingest --source-uri '<src>' --source-table 'public.companies' \
  --dest-uri 'hubspot://?api_key=pat_test_12345' \
  --dest-table 'companies?id_property=hs_object_id&id_column=company_id&on_error=skip'

# Link contacts to companies
ingestr ingest --source-uri '<src>' --source-table 'public.contact_company_links' \
  --dest-uri 'hubspot://?api_key=pat_test_12345' \
  --dest-table 'associations?from=contacts&to=companies&from_id_column=contact_id&to_id_column=company_id'
```
