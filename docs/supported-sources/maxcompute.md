# MaxCompute

MaxCompute is Alibaba Cloud's distributed data processing and warehousing platform.

ingestr supports MaxCompute as both a source and destination. The `odps` URI scheme is also accepted.

## URI format

```plaintext
maxcompute://<access_id>:<access_key>@<host>/<project>?schema=<schema>
```

You can also provide the full endpoint as a query parameter:

```plaintext
maxcompute://<access_id>:<access_key>@placeholder/<project>?endpoint=https://service.<region>.maxcompute.aliyun.com/api&schema=<schema>
```

URI parameters:

- `access_id`: Alibaba Cloud access key ID. It can also be supplied with `ALIBABA_CLOUD_ACCESS_KEY_ID` or `ODPS_ACCESS_ID`.
- `access_key`: Alibaba Cloud access key secret. It can also be supplied with `ALIBABA_CLOUD_ACCESS_KEY_SECRET` or `ODPS_ACCESS_KEY`.
- `host`: MaxCompute endpoint host. If `endpoint` is supplied, this can be any non-empty placeholder host.
- `project`: MaxCompute project name. This can be supplied in the URI path or as `project`.
- `schema` (optional): MaxCompute schema name.
- `endpoint` (optional): Full MaxCompute endpoint URL.
- `tunnel_endpoint` (optional): Tunnel endpoint.
- `tunnel_quota_name` (optional): Tunnel quota name.
- `sts_token` (optional): STS token.

The same URI structure can be used for sources and destinations.

## Example

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@localhost:5432/app" \
  --source-table "public.events" \
  --dest-uri "maxcompute://<access_id>:<access_key>@service.cn-hangzhou.maxcompute.aliyun.com/<project>?protocol=https&schema=analytics" \
  --dest-table "events"
```

## Supported destination strategies

When using MaxCompute as a destination, ingestr supports `replace`, `append`, and `truncate+insert`.

`merge`, `delete+insert`, and `scd2` are not supported for MaxCompute destinations. ingestr also does not expose destination transactions for MaxCompute. The test emulator may use a local SQL transaction internally, but real MaxCompute destinations fail transaction requests instead of returning a no-op transaction wrapper.
