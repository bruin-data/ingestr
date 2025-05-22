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

- `objects`: Objects are the data types used to store facts about your customers. Fetches all objects.
- `records:{object_api_slug}`: Fetches all records of an object. For example: `records:companies`
- `lists`: Fetches all lists
- `list_entries:{list_id}`: Lists all items in a specific list. For example: `list_entries:8abc-123-456-789d-123`
- `all_list_entries:{object_api_slug}`: Fetches all the lists for an object, and then fetches all the entries from that list. For eg:  Fetches all lists for an object, and then fetches all entries from those lists. For example: `all_list_entries:companies`

Use this as `--source-table` parameter in the `ingestr ingest` command.