# HubSpot

[HubSpot](https://www.hubspot.com/) is a customer relationship management software that helps businesses attract visitors, connect with customers, and close deals.

ingestr supports HubSpot as both a source and a destination.

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

ingestr can write CRM records and record-to-record associations back into HubSpot (reverse ETL). Each source row becomes one HubSpot record (or one association link), sent in bulk through HubSpot's batch APIs.

The private app token you write with needs **write** scopes on the objects you target (e.g. `crm.objects.contacts.write`). For custom objects, add the schema read scope so ingestr can resolve the object by name.

### URI format

```
hubspot://?api_key=<private-app-token>
```

The parameters are the same as the source — provide either `api_key` or `service_key`.

### What gets written

The base of `--dest-table` (before the `?`) names the object type: a built-in object (`contacts`, `companies`, `deals`, `products`, `line_items`, …) or a custom object by its name. By default every source column is written as a HubSpot property whose **internal name** matches the column name — rename a column to a different property with [`--columns`](#column-mapping). Properties are always addressed by internal name (e.g. `numberofemployees`), never by display label; the column name (after any rename) must equal the target property's internal name. ingestr's own `_ingestr_loaded_at` and `_ingestr_run_id` columns are never sent.

The **write behaviour is chosen with `--incremental-strategy`**, which is **required** — HubSpot has no default strategy:

| Strategy | Behaviour |
| -------- | --------- |
| `merge` | Upsert — update the matching record, or create it if none matches. This is the usual choice. |
| `update` | Update matching records only; rows with no match are rejected (never created). Unmatched rows follow [`--reject-mode`](#reverse-etl-options) — use `--reject-mode skip` to report them but still succeed. |
| `append` | Always create a new record, never match. Re-running over the same rows creates duplicates — scope each run to new rows with `--interval-start`/`--interval-end`, or use `merge` to stay idempotent. On a unique-property collision HubSpot returns a conflict, handled per [`--reject-mode`](#reverse-etl-options). |
| `delete` | Archive (soft-delete) the matching records. Rows with no match follow [`--reject-mode`](#reverse-etl-options) — use `--reject-mode skip` to report them but still succeed. |
| `replace` | Mirror — upsert every source row, then archive any record whose match value is **not** in the source. Costly: it scans every record of the object type on each run, so it is slow and expensive on large objects. |

#### Matching records

Upsert/update/delete match existing records on two independent things — **which HubSpot property** to match on, and **which source column** carries the value:

**Match property (HubSpot side)** — set with `id_property=<property>` on the dest-table:
- **`merge`** and **`replace`** require it explicitly, and it must be a **unique property** — not `hs_object_id`, since HubSpot has no upsert-by-record-id (use `update` to match existing records by id). There is no built-in default, because the right unique key differs per object (contacts have `email`, products `hs_sku`, companies none). If it's missing, the run fails fast.
- **`update`** and **`delete`** default it to **`hs_object_id`** (HubSpot's record id) when you don't set one.
- `id_property` must be the property's **internal name**, not its display label (e.g. `numberofemployees`, not "Number of Employees").

**Source column (value side)** — the column supplying the match value, named with `--primary-key`:
- **`merge`** and **`replace`** require it explicitly (associations need two: `--primary-key k1,k2`).
- **`update`** and **`delete`** default it to the **`hs_object_id`** column when you don't pass one, so a source keyed by HubSpot record id needs no flag; pass `--primary-key <col>` to match on a different column (e.g. `email`).

For **`update`** and **`delete`**, the match property does not have to be unique. If the property is non-unique (e.g. `company_name`), ingestr finds **every** record with that value via the Search API and updates/archives all of them — so you can, for example, flag every contact at a company in one row. ingestr detects uniqueness automatically from the property definition, so no extra flag is needed. (`merge`/`replace` still require a unique property, since upsert/mirror match one record.)

```sh
# Upsert contacts, matching on email
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.customers" \
  --dest-uri "hubspot://?api_key=pat-xxxx" \
  --dest-table "contacts?id_property=email" \
  --primary-key email \
  --incremental-strategy merge
```

```sh
# Archive the contacts listed in the source, matched by email
ingestr ingest \
  --source-uri "csv://churned.csv" \
  --source-table "churned" \
  --dest-uri "hubspot://?api_key=pat-xxxx" \
  --dest-table "contacts?id_property=email" \
  --primary-key email \
  --incremental-strategy delete
```

> [!WARNING]
> `replace` is a full object-wide mirror: it archives **every** record of the object type that is not in your source (including records with no match value, and ones created via the UI or other integrations). Use it only when the source is the complete, sole system of record for that object. A run with **0 source rows** archives nothing; to remove records intentionally, use `delete`.

### Reverse-ETL options

These flags apply only when HubSpot is the **destination** (reverse ETL); on any other destination they are rejected with an error.

#### `--reject-mode`

Controls how a row HubSpot can't apply (no match, or rejected) is handled. One of:

- **`fail`** *(default)* — send every valid row, then fail the run listing the rejects.
- **`fail_fast`** — stop at the first bad row.
- **`skip`** — send every valid row, report the rejects, and still succeed (exit 0).

Under `fail` and `skip` every valid row is written — they differ only in whether the run reports failure at the end. `fail_fast` is the exception: it stops at the first bad row, so rows not yet processed are not written.

Only **per-record** problems count as rejects that `skip` tolerates: a row with no matching record (`update`/`delete`/associations) and a row HubSpot rejects on its own value (a validation error or a unique-property conflict). **Systemic failures always abort the run regardless of `--reject-mode`.**

> [!NOTE]
> HubSpot's batch writes aren't transactional, so `fail`/`fail_fast` may have written some records before the run stops.

#### `--write-nulls`

- **`true`** *(default)* — a null source cell is written through as empty, clearing the field in HubSpot.
- **`false`** (`--write-nulls=false`) — a null source cell is omitted, leaving the existing HubSpot value untouched.

Only affects strategies that write property values — `merge`, `update`, `replace`, `append`. It has no effect on `delete` (archives by id) or associations (link records), which send no properties.

### Column mapping

When a source column name differs from the HubSpot property name, rename it with `--columns` using `dest_property::source_column` (comma-separated for several). `dest_property` is the property's **internal name** (not its display label):

```sh
--columns 'firstname::first_name,hs_lead_status::status'
```

Only **renaming** is allowed for HubSpot — the property type is fixed on HubSpot's side, so a `--columns` entry that includes a type (e.g. `lead_score:int:score`) is rejected before the run starts. The right-hand side is the **source column** and must exist in the source: unlike SQL destinations (where an override creates the target column), HubSpot can't create a property from an override, so naming a source column that isn't there — e.g. writing the pair backwards as `first_name::firstname` — fails fast instead of silently doing nothing.

### Properties must already exist

HubSpot does **not** create properties on the fly. Every column you write must map to a property that already exists on the object (built-in or one you created in HubSpot beforehand). A row referencing an unknown property is rejected by HubSpot and handled per `--reject-mode`.

### Multi-select properties

For multi-select (checkbox) properties, HubSpot uses a semicolon-separated list of option values, and ingestr sends your source value through **verbatim** — it adds no special handling:

- `"CHAMPION;DECISION_MAKER"` sets the property to exactly those two options (replacing any current selection).
- A **leading** semicolon appends instead of replacing: `";BLOCKER"` adds `BLOCKER` to whatever is already selected.

### Associations

Link two objects by naming both sides of the dest-table as `obj1+obj2` and giving the two source key columns as `--primary-key k1,k2` (obj1 first, obj2 second):

```sh
# Link contacts to companies
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.contact_company" \
  --dest-uri "hubspot://?api_key=pat-xxxx" \
  --dest-table "contacts+companies?id_property=email,hs_object_id" \
  --primary-key email,company_id \
  --incremental-strategy merge
```

The dest-table (`obj1+obj2?...`) accepts these optional parameters:

- `id_property=fromProp,toProp` — the property each side matches on (empty = the value is already a HubSpot record id).
- `label=<name>` — apply a named association label, resolved to its type id.
- `association_type=<id>` — the numeric association type id, as an alternative to `label`.
- `association_category=<cat>` — the category for a numeric `association_type` (e.g. `HUBSPOT_DEFINED`, `USER_DEFINED`); resolved automatically when omitted.

Behaviour:

- Match values are resolved to record ids before linking. A non-empty key that matches no record is a not-found reject handled per `--reject-mode`. An empty cell is skipped for `merge`/`delete`; under `replace` an empty `obj2` cell for a present `obj1` clears that record's links.
- A key column holding a list/array links the row to every element (one contact to many companies in a single row); repeated pairs **within a row** are de-duplicated. HubSpot's link creation is idempotent, so the same pair across rows is harmless.
- For `delete` and `replace`, a `label`/`association_type` **scopes the unlink to that one type** — other labels between the pair survive. Without one, the unlink removes **every** type between the two records.

Association strategies:

| Strategy | Behaviour |
| -------- | --------- |
| `merge` | Add the links in the source. |
| `delete` | Remove (unlink) the links in the source. |
| `replace` | Mirror — for every source `obj1` record, make its links exactly match the source, removing any others. An `obj1` row with an **empty `obj2` cell** means "this record should have no links": all of its existing associations are removed. |

Only `merge`, `delete`, and `replace` are supported for associations. `update` and `append` fail fast with an error.

Either side can be a [custom object](#writing-to-custom-objects). This example links contacts to a custom `licenses` object with a named label:

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.contact_licenses" \
  --dest-uri "hubspot://?api_key=pat-xxxx" \
  --dest-table "contacts+licenses?id_property=email,license_key&label=Primary License" \
  --primary-key email,license_id \
  --incremental-strategy merge
```

### Writing to custom objects

Use a custom object anywhere a built-in object is accepted (records and associations). ingestr resolves the name to its `objectTypeId` via HubSpot's schemas API and caches it, so you can address it by any of:

- internal name — `license`
- fully-qualified name — `p123_license`
- the `objectTypeId` itself — `2-123`

Address custom objects only by these identifiers, which HubSpot keeps unique. Display labels (singular/plural) are **not** accepted: HubSpot allows two schemas to share a label, so a label could resolve to the wrong object.

Everything else — strategies, matching (`id_property`), `--reject-mode`, associations — works exactly as for built-in objects. For example, upsert a custom object whose internal name is `license` on a unique `sku`:

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.licenses" \
  --dest-uri "hubspot://?api_key=pat-xxxx" \
  --dest-table "license?id_property=sku" \
  --incremental-strategy merge
```
