# Attio
[Attio](https://attio.com/) is an AI-native CRM platform that helps companies build, scale, and grow their business.

ingestr supports Attio as a source and as a [destination](#attio-as-a-destination).

## URI format

The URI format for Attio is as follows:

```plaintext
attio://?api_key=<api_key>
```

URI parameters:
- `api_key`: the API key used for authentication with the Attio API

## Setting up a Attio Integration

You can find your Attio API key by following the guide [here](https://attio.com/help/apps/other-apps/generating-an-api-key).

Let's say your `api_key` is key_123, here's a sample command that will copy the data from Attio into a DuckDB database:


```bash
ingestr ingest \
--source-uri 'Attio://?api_key=key_123' \
--source-table 'objects' \
--dest-uri duckdb:///attio.duckdb \
--dest-table 'dest.objects'
```

## Tables

Attio source supports ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|----------|--------------|---------|
| [objects](https://docs.attio.com/rest-api/endpoint-reference/objects/list-objects) | - | - | replace | Objects are the data types used to store facts about your customers. Fetches all objects. Full reload on each run. |
| [records:{object_api_slug}](https://docs.attio.com/rest-api/endpoint-reference/records/list-records) | - | - | replace | Fetches all records of an object. For example: `records:companies`. Full reload on each run. |
| [lists](https://docs.attio.com/rest-api/endpoint-reference/lists/list-all-lists) | - | - | replace | Fetches all lists. Full reload on each run. |
| [list_entries:{list_id}](https://docs.attio.com/rest-api/endpoint-reference/entries/list-entries) | - | - | replace | Lists all items in a specific list. For example: `list_entries:8abc-123-456-789d-123`. Full reload on each run. |
| [all_list_entries:{object_api_slug}](https://docs.attio.com/rest-api/endpoint-reference/entries/list-entries) | - | - | replace | Fetches all the lists for an object, and then fetches all the entries from that list. For example: `all_list_entries:companies`. Full reload on each run. |

Use this as `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> Attio does not support incremental loading, which means ingestr will do a full-refresh.

## Attio as a destination

ingestr can write rows from any source back into Attio (reverse ETL). Each source row creates, updates or deletes one Attio record.

### URI format

The destination URI is the same as the source URI:

```plaintext
attio://?api_key=<api_key>
```

The access token needs these scopes:
- **Records**: Read-write
- **Object Configuration**: Read

### Dest-table format

```plaintext
<object>?matching_attribute=<attribute>
```

Dest-table parameters:
- `<object>`: the object to write to, by its API slug: `people`, `companies`, `deals`, `users`, `workspaces`, or a custom object's slug.
- `matching_attribute`: the attribute used to find existing records. Needed for `merge` and `replace`. See [Matching records](#matching-records).

### Quick start

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.customers" \
  --dest-uri "attio://?api_key=<api_key>" \
  --dest-table "people?matching_attribute=email_addresses" \
  --primary-key email \
  --incremental-strategy merge
```

For every row, this finds the person whose email address equals the row's `email`: if one exists it is updated, otherwise it is created. The other source columns (`job_title`, `description`, …) are written to the attributes with the same slug.

### Objects and attributes

- Each source column is written to the attribute with the same **API slug**, e.g. `job_title` or `email_addresses`. Slugs are not case-sensitive. Find them in Attio under Settings → Objects → *object* → Attributes.
- ingestr doesn't create objects or attributes. A source column that isn't an attribute, or is read-only (such as an enriched system attribute), stops the run **before anything is written**. Drop such columns, for example with `--sql-exclude-columns`.
- A source column named `record_id` is only used to find records, never written. ingestr's own `_ingestr_loaded_at` and `_ingestr_run_id` columns are never sent.

### Strategies

`--incremental-strategy` is **required**:

| Strategy | What it does | What it needs |
|---|---|---|
| `merge` | Updates the matching record, or creates it if there is none | `matching_attribute=` and `--primary-key` |
| `update` | Updates matching records only; a row with no match is rejected | nothing when matching by `record_id` |
| `append` | Always creates new records | nothing |
| `delete` | Deletes matching records | nothing when matching by `record_id` |
| `replace` | Like `merge`, then deletes every record that isn't in the source | `matching_attribute=` and `--primary-key` |

- **`append`** never matches, so re-running it creates duplicates, unless a unique attribute refuses them. `matching_attribute=` isn't allowed with it; `--primary-key` is accepted and only names rejected rows in the report.
- **`delete`** deletes records permanently. Attio has no recycle bin for records deleted through the API.
- In `merge`, a row with an empty match value creates a new record.

> [!WARNING]
> `replace` permanently deletes **every** record of the object that isn't in your source, including ones created in Attio or by other tools. Use it only when your source is the complete list. As a safety net, a run with **0 source rows** deletes nothing, and with the default `--reject-mode fail` a run with any rejected row deletes nothing either. Under `skip`, a run where every row was rejected also deletes nothing.

### Matching records

Two settings decide which record a row updates or deletes:

| Setting | Set with | Example |
|---|---|---|
| The **Attio attribute** to match on | `matching_attribute=` in `--dest-table` | `people?matching_attribute=email_addresses` |
| The **source column** that holds the value | `--primary-key` | `--primary-key email` |

**`merge` and `replace`** need both, and the attribute must be **unique** in Attio, e.g. `email_addresses` on people or `domains` on companies. To match on your own id, create a text attribute, tick **Unique**, and fill it from the source. `record_id` can't be used, because Attio can't create a record with an id you choose.

When the match attribute holds several values (like `email_addresses` or `domains`), `merge` adds the row's value to the record's list instead of replacing it.

Email addresses and domains match regardless of case. A unique text attribute you create is case-sensitive: `A-1` and `a-1` are two different records, and `A-1` never matches `a-1`.

**`update` and `delete`** match by Attio's record id by default, using a source column named `record_id`:

```sh
# Delete the people listed in the source by their Attio record id
ingestr ingest \
  --source-uri "csv://churned.csv" \
  --source-table "churned" \
  --dest-uri "attio://?api_key=<api_key>" \
  --dest-table "people" \
  --incremental-strategy delete
```

They can also match on any text, number, email, domain or select attribute with `matching_attribute=`, even one that isn't unique. Then **every** matching record is updated or deleted.

### Linking records

Attio links records through **record reference** attributes, such as a person's `company` or a deal's `associated_company`. Write them like any other column, in one of these ways:

| You have | Column name | Example value |
|---|---|---|
| A company's domain or a person's email | The attribute, e.g. `company` | `acme.com` |
| A unique value on the linked record | `<attribute>.<unique attribute>`, e.g. `company.domains` | `acme.com` |
| The linked record's Attio id | `<attribute>.record_id`, e.g. `company.record_id` | `99a03ff3-0435-47da-95cc-76b2caeb4dab` |

- The column name is what counts. Name the column this way in your source query (`SELECT company_domain AS "company.domains"`), or rename it with `--columns`, e.g. `--columns 'company.domains::company_domain'`.
- If the attribute can point to more than one object, put the object in the middle: `<attribute>.<object>.<unique attribute>`, e.g. `related.people.email_addresses`. A column without the object stops the run before writing and lists the objects to choose from. This applies to the final name, whether it comes from the source or from `--columns`.
- A list value links to several records at once, for attributes that allow many.
- The linked record must already exist, so load companies before the people that point to them.
- Attio links a new person to a company from their email's domain on its own, creating the company if needed. A `company` column you write takes precedence.
- **Unlinking:** an empty (null) value removes the link. Pass `--write-nulls=false` to leave existing links alone.

### Names

A person's `name` can be written in two ways:
- As one text column in the form `Last, First`, e.g. `Lovelace, Ada`. Text without a comma is taken as the first name only.
- As separate columns `name.first_name`, `name.last_name` and, optionally, `name.full_name`. Without a full name, ingestr joins the first and last names.

Attio stores a name as one value, so every write replaces the whole name: a row with only `name.first_name` clears the last name. When every name column is empty, the name is cleared, or left as it is with `--write-nulls=false`.

### Reverse-ETL options

These flags only apply when Attio is the destination.

#### `--reject-mode`

What to do with a row Attio refuses, or that matches no record:

| Mode | Behaviour |
|---|---|
| `fail` *(default)* | Write every valid row, then fail the run and list the rejected rows |
| `fail_fast` | Stop at the first rejected row |
| `skip` | Write every valid row, list the rejected rows, and succeed |

One bad row never blocks the others. Each rejected row is listed with Attio's reason and its key. Problems that affect the whole run, such as an invalid access token or a missing scope, always stop it, whatever the mode.

> [!NOTE]
> Attio writes can't be rolled back, so with `fail` or `fail_fast` some records may already be written when the run stops.

#### `--write-nulls`

- **`true`** *(default)*: an empty (null) source value clears the attribute in Attio.
- **`false`** (`--write-nulls=false`): an empty source value is skipped, and the attribute keeps its current value.

It has no effect on `delete`.

### Column mapping

If a source column's name differs from the Attio attribute, rename it with `--columns 'attribute::source_column'`. Separate several with commas:

```sh
--columns 'email_addresses::email,company.domains::company_domain'
```

- Only renaming is allowed. Attribute types are set in Attio, so an entry with a type is rejected.
- `--primary-key` can name either the source column or its new name. With `--columns 'email_addresses::email'`, both `--primary-key email_addresses` and `--primary-key email` work.
- Names are sent as written, so `--schema-naming` is ignored.

### Values

- Numbers, booleans, text, dates and timestamps are sent in the form Attio expects. Timestamps are sent in UTC, and Attio keeps them to the millisecond.
- Select and status attributes take the option's title, e.g. `Lead`.
- A list value writes every item to an attribute that allows several values (tags, email addresses, domains). On `merge` and `update`, the list replaces the record's current values.
- A JSON value is sent as-is, so any value format Attio documents (e.g. a full location object) can be written from a JSON column.

### Speed

Attio accepts one record per request and about 25 writes per second, so ingestr writes roughly 1,000 records a minute. `update` and `delete` on an attribute other than `record_id` also look each value up first.
