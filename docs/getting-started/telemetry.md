---
outline: deep
---

# Telemetry
ingestr uses a basic form of usage telemetry to understand adoption at a high level.
- It uses a protected, pseudonymous machine ID to keep track of the number of unique installations. On machines where that ID cannot be read, ingestr falls back to a hash derived from the OS name and the hostname.
- It sends the following information:
  - ingestr version
  - machine ID
  - OS information: OS, architecture, Go version
  - command executed
  - success/failure
  - source and destination connector types, such as `postgres` or `bigquery`
  - execution mode, such as batch, stream, or CDC stream
  - non-identifying feature flags, such as full refresh, multi-table, masking, and schema inference
  - coarse duration and CPU-capacity buckets
  - coarse deployment and invocation categories, such as Kubernetes, container, CI, or interactive

The information collected here is used to understand the usage of ingestr and to improve the product. We use [Rudderstack](https://www.rudderstack.com/) to collect the events. The telemetry payload is designed not to contain PII.

ingestr sends one `command_finished` event after a command exits. Long-running server and streaming commands do not send periodic telemetry.

Telemetry properties are selected from fixed values. ingestr does not send connection URIs, credentials, hostnames, IP addresses, table or column names, queries, raw errors, arbitrary environment-variable values, or company identifiers.

Tools that embed ingestr can set `INGESTR_TELEMETRY_INVOKER` to identify themselves. Only the known values `bruin_cli`, `bruin_cloud`, and `ingestr_server` are reported; anything else is recorded as `other`, and an unset variable is recorded as `direct`.

The questions we answer by these simple events are:
- How many unique installations are using ingestr?
- How many times is ingestr being used?
- Which execution environments and features are commonly used?
- Which broad classes of commands fail?

## Disabling telemetry
If you'd like to turn off the telemetry, simply set the `INGESTR_DISABLE_TELEMETRY` environment variable to `true`.
The legacy `DISABLE_TELEMETRY` environment variable is also supported.
