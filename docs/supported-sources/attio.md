# Attio
[Attio](https://attio.com/) is an AI-native CRM platform that helps companies build, scale, and grow their business.

ingestr supports Attio as a source.

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