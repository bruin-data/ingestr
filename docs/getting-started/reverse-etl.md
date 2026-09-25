---
outline: deep
---

# Reverse ETL

Reverse ETL is ingestion in the other direction: instead of pulling data into your warehouse, ingestr pushes rows from the warehouse back into an operational tool — a CRM, a marketing app — through that tool's API.

- The warehouse stays the source of truth.
- Any ingestr source can feed a reverse-ETL destination.
- Rows are read the same way as any other run (full table, or a window via [incremental loading](/getting-started/incremental-loading.md)). Only the destination changes: each row becomes an API call, not a row in a table.

## How it's different from a warehouse destination

A warehouse destination stages data, runs SQL, and swaps tables. A reverse-ETL destination does none of that:

- No staging table, no SQL, no atomic swap.
- Each source row is written to the target API as one record (or one link). It usually affects a single remote record, but not always — matching on a non-unique field can update or delete every record that matches (see [Matching records](#matching-records)).
- Rows are sent in bulk through the API's batch endpoints, within its rate limits.

Two things follow from this:

- **The API decides what's possible.** Which strategies work and what you can match on depend entirely on the target API. ingestr only exposes what the API actually supports.
- **Writes aren't transactional.** A run that fails partway may already have written some records, and ingestr can't roll them back. It reports the rows the API rejected (see [`--reject-mode`](#reject-mode)).

## Supported destinations

| Destination | What you can write |
| ----------- | ------------------ |
| [HubSpot](/supported-sources/hubspot.md#hubspot-as-a-destination) | CRM records (contacts, companies, deals, custom objects) and associations |
| [CleverTap](/supported-sources/clevertap.md#what-to-upload) | User profiles and events |
| [Salesforce](/supported-sources/salesforce.md#salesforce-as-a-destination) | sObject records (standard and custom objects) |

Each destination's page has its own URI, object types, and quirks. This page covers what they share.

## Strategies

Pick the write behaviour with `--incremental-strategy`. There's often no sensible default, so some destinations require it explicitly. Where a destination honours the strategy, the names map to API operations like this:

- `merge` — upsert: update the match, or create it if there's none. The usual choice.
- `update` — update matches only; a row with no match is rejected, never created.
- `append` — always create. Re-running duplicates rows unless you scope each run to new rows.
- `delete` — remove the matches. Whether that's a soft-delete (archive, recoverable) or a hard-delete (permanent) depends on the destination — some APIs only offer one. Check its page.
- `replace` — mirror: upsert every source row, then remove anything **not** in the source, using the same delete semantics as `delete`.

Notes:

- Not every destination supports every strategy — check its page.
- Impossible combinations are rejected before the run starts, not silently mishandled.
- **Some destinations don't act on the strategy at all.** CleverTap, for example, always upserts profiles and always appends events regardless of the strategy you pass — so `merge`, `delete`, and `replace` don't perform the operations above (a `delete` or `replace` run can succeed while removing nothing). The strategy semantics here apply only where the destination's page says the strategy is honoured.

::: warning
On a destination that supports deletion, `replace` removes **every** record that isn't in your source — including ones created by hand or by other tools. Use it only when the source is the complete system of record. A run with 0 source rows removes nothing (a safety guard). To remove specific records, use `delete`.
:::

## Matching records

`merge`, `update`, and `delete` need to find the existing record. That's two separate things:

- **Remote field to match on** — a destination-side property, set on `--dest-table` (e.g. `id_property=email` for HubSpot). Each destination names this parameter after its own API, so Salesforce calls it `external_id`. Check the destination's page for the name it uses.
- **Source column with the value** — set with `--primary-key`.

They're independent — `--dest-table "contacts?id_property=email" --primary-key customer_email` matches the `email` property against your `customer_email` column.

The exact identity rules differ per destination — what counts as a valid match field, and whether more than one key is allowed, depends on the API. See each page.

## Shared flags

Both flags apply only to reverse-ETL destinations. On a warehouse destination they're rejected.

### `--reject-mode` {#reject-mode}

What happens to a row the API can't apply (no match, or rejected on its value). All three modes write the valid rows they process — they differ in how far they get and in the exit status:

- `fail` *(default)* — send every valid row, then fail at the end listing the rejects.
- `fail_fast` — stop at the first bad row; valid rows before it are written, rows after it aren't sent.
- `skip` — send every valid row, report the rejects, exit 0.

- Only per-record problems are skippable. Systemic failures (auth, malformed request) always abort, whatever the mode.

### `--write-nulls`

What a `NULL` source cell does to a field:

- `true` *(default)* — write it through, clearing the field.
- `false` (`--write-nulls=false`) — omit the cell, leaving the stored value untouched.

Only affects strategies that write field values (`merge`, `update`, `append`, `replace`). No effect on `delete`, or on writes that don't carry editable fields.

## Column mapping

Rename a column to a differently-named property with `--columns`, using `dest_property::source_column`:

```sh
--columns 'firstname::first_name,lifecyclestage::status'
```

- Rename only — reverse-ETL destinations own their property types. Some destinations reject an entry that carries a type; check its page.
- Renaming happens **before** the destination sees the row, so `--primary-key` must name the **renamed** column. With `--columns 'Ext_Id__c::customer_ref'`, pass `--primary-key Ext_Id__c`, not `customer_ref`.
- Whether an unknown property is accepted also depends on the destination: some create attributes on the fly, others require the property to already exist. Check its page.

## Example

Upsert warehouse contacts into HubSpot, matching on email:

```sh
ingestr ingest \
  --source-uri "bigquery://my-project/analytics?credentials_path=/creds.json" \
  --source-table "analytics.customers" \
  --dest-uri "hubspot://?api_key=pat-xxxx" \
  --dest-table "contacts?id_property=email" \
  --primary-key email \
  --incremental-strategy merge
```

Swap the destination URI and table for CleverTap — see each destination's page for the object types and parameters specific to it.
