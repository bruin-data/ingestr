# HTTP

ingestr can read CSV, JSON, JSONL, and Parquet files directly from HTTP and HTTPS URLs.

## URI format

Use the file URL as `--source-uri`. Query parameters remain part of the request URL, so signed download URLs work unchanged.

```sh
ingestr ingest \
  --source-uri 'https://example.com/exports/companies.csv' \
  --source-table 'companies' \
  --dest-uri 'duckdb:///local.duckdb' \
  --dest-table 'main.companies'
```

HTTP source options use an `#ingestr:` fragment. URL fragments are not sent to the server, which keeps source configuration separate from the file URL's query parameters:

```text
https://example.com/data.csv?signature=server-value#ingestr:retries=5
```

Percent-encode option values that contain `&`, `=`, `#`, spaces, or other URL-reserved characters.

## Format selection

The source selects the decoder in this order:

1. An explicit format hint on `--source-table`.
2. The final URL suffix after redirects.
3. The response `Content-Type`.

| Source-table hint | URL suffixes | Content types |
|---|---|---|
| `#csv` | `.csv`, `.csv.gz` | `text/csv`, `application/csv` |
| `#csv_headless` | — | — |
| `#json` | `.json`, `.json.gz` | `application/json` |
| `#jsonl` | `.jsonl`, `.ndjson`, and gzipped variants | `application/jsonl`, `application/x-ndjson`, `application/ndjson`, `application/x-jsonlines` |
| `#parquet` | `.parquet`, `.parquet.gz` | `application/vnd.apache.parquet`, `application/x-parquet` |

Use a hint when the URL and content type are ambiguous:

```sh
--source-uri 'https://example.com/export?id=42' \
--source-table 'companies#jsonl'
```

CSV encoding hints match blobstore sources and can be combined in either order:

```sh
--source-table 'companies#csv,encoding=windows-1252'
```

For a CSV without a header, use `#csv_headless`. Define names and optional types with `--columns`; otherwise columns are named `unknown_col_0`, `unknown_col_1`, and so on.

## Authentication and headers

### Bearer authentication

```sh
--source-uri 'https://example.com/data.csv#ingestr:bearer_token=TOKEN'
```

### Basic authentication

Basic credentials can use URL user information:

```sh
--source-uri 'https://username:password@example.com/data.csv'
```

or source options:

```sh
--source-uri 'https://example.com/data.csv#ingestr:basic_user=username&basic_password=password'
```

### Custom headers

Prefix each header name with `header.`. Repeat a key to send multiple values:

```sh
--source-uri 'https://example.com/data.jsonl#ingestr:header.X-API-Key=TOKEN&header.X-Tenant=acme'
```

Credentials, option fragments, header values, and query values are redacted from debug logs. On a redirect, configured authentication and custom headers are retained only when the scheme and host are unchanged. They are stripped before following a cross-origin redirect. The source follows at most 10 redirects.

## Retries, streaming, and resume

CSV, JSON, and JSONL responses are decoded as they arrive. Batches can reach the destination before the download completes, and memory is bounded by the configured batch size. Gzip responses and files ending in `.gz` are decompressed as streams.

Parquet requires random access. The HTTP response is therefore streamed to a temporary file and decoded with the existing Parquet file reader. This bounds heap usage at the cost of temporary disk space; the temporary file is removed after the read.

HTTP 408, 429, and 5xx responses and transport failures are retried with bounded exponential backoff. `Retry-After` is honored. There are three retries after the initial request by default:

```sh
--source-uri 'https://example.com/data.csv#ingestr:retries=5'
```

`retries` accepts values from 0 through 10. Waiting and active requests stop promptly when ingestion is cancelled.

If a response is interrupted after bytes have been decoded, ingestr resumes only when it can preserve file identity:

- the initial response supplied a strong `ETag` or `Last-Modified` validator;
- the server accepts a `Range` request and returns `206 Partial Content`;
- `Content-Range`, total length, and validators remain consistent.

The resume request uses `If-Range`. If any condition is not met, ingestion fails rather than restarting the file and emitting duplicate rows. Resume state is limited to the current ingestion process; it is not persisted between runs.

## Validation and conditional requests

When the server supplies `Content-Length`, ingestr verifies that the complete response has that length, including across resumed requests.

An optional SHA-256 checksum validates the exact response bytes before file-level gzip decompression:

```sh
--source-uri 'https://example.com/data.csv#ingestr:checksum=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
```

Conditional validators can be supplied explicitly:

```sh
--source-uri 'https://example.com/data.csv#ingestr:if_none_match=%22version-7%22'
--source-uri 'https://example.com/data.csv#ingestr:if_modified_since=Wed%2C+21+Oct+2015+07%3A28%3A00+GMT'
```

A `304 Not Modified` response returns `ErrNotModified` and stops ingestion before the destination is changed. ingestr does not persist validators automatically, because doing so without a destination-aware state contract could incorrectly skip or replace data.

## Response metadata

After a response starts, `HTTPSource.Metadata()` exposes:

- the final URL after redirects;
- `ETag`;
- `Last-Modified`;
- content length (`-1` when unknown);
- content type.

The same non-secret values are shown with `--debug`; URLs in logs have user information, fragments, and query values redacted.

## Source options

| Option | Description |
|---|---|
| `bearer_token` | Bearer token; cannot be combined with Basic authentication. |
| `basic_user` / `basic_password` | HTTP Basic credentials. |
| `header.<Name>` | Custom request header. Transport-managed headers such as `Range`, `Content-Length`, and `Accept-Encoding` cannot be overridden. |
| `retries` | Retries after the initial request, from 0 to 10. Default: 3. |
| `checksum` | Expected checksum in `sha256:<64 hex characters>` form. |
| `if_none_match` | Value for `If-None-Match`. |
| `if_modified_since` | Value for `If-Modified-Since`. |
