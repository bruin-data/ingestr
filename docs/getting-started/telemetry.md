---
outline: deep
---

# Telemetry
ingestr uses a very basic form of **anonymous telemetry** to be able to keep track of the usage on a high-level.
- It uses anonymous machine IDs that are hashed to keep track of the number of unique users.
- It sends the following information:
  - ingestr version
  - machine ID
  - OS information: OS, architecture, Python version
  - command executed
  - success/failure

The information collected here is used to understand the usage of ingestr and to improve the product. We use [Rudderstack](https://www.rudderstack.com/) to collect the events and we don't store any PII. 

The specific events that are sent are:
- command triggered
- command finished

The questions we answer by these simple events are:
- How many unique users are using ingestr?
- How many times is ingestr being used?

## Disabling telemetry
If you'd like to turn off the telemetry, simply set the `INGESTR_DISABLE_TELEMETRY` environment variable to `true`.