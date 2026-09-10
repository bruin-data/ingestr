# BambooHR

[BambooHR](https://www.bamboohr.com/) is an HR platform for employee records, time off,
time tracking, and related workforce data.

ingestr supports BambooHR as a source.

## URI format

```text
bamboohr://<company-domain>?api_key=<api-key>
bamboohr://<company-domain>?access_token=<oauth-access-token>
```

- `company-domain` is the part before `.bamboohr.com` in the company URL. For
  `https://acme.bamboohr.com`, use `acme`.
- `api_key` is a BambooHR API key. BambooHR applies the permissions of the user who
  created the key, so the source only returns fields and employees that user can access.
- `access_token` is an OAuth bearer token. Use either `api_key` or `access_token`, not both.
- `timezone` is the company's IANA timezone, such as `America/Denver`. It is required for
  `timesheet_entries`, whose date boundaries BambooHR interprets in the company timezone.

To create a key, sign in to BambooHR, open the user menu in the lower-left corner, choose
**API Keys**, and create a new key. See BambooHR's
[API getting-started guide](https://documentation.bamboohr.com/docs/getting-started) for details.
The same guide documents the OAuth authorization flow for hosted or third-party integrations.

API-key authentication is intended for a customer's own, internally operated integration. Do
not share an API key with a hosted third-party service; use OAuth for that scenario, as required
by BambooHR's [Developer Terms](https://www.bamboohr.com/legal/developer-terms-of-service).

## Example

```sh
ingestr ingest \
  --source-uri 'bamboohr://acme?api_key=secret' \
  --source-table 'employees?fields=workEmail,hireDate,departmentName,locationName' \
  --dest-uri duckdb:///bamboohr.duckdb \
  --dest-table 'main.employees'
```

The `employees` table always includes BambooHR's default employee fields. Add a
comma-separated `fields` table parameter to request other standard or custom field aliases
that the API-key owner can read. Discover account-specific aliases with the
`employee_fields` table.

## Tables

| Table | Primary key | Incremental key | Strategy | Details |
| --- | --- | --- | --- | --- |
| `employees` | `employeeId` | - | replace | Complete employee roster, including active and inactive employees, with optional extra fields |
| `employee_directory` | `id` | - | replace | Published company directory, including future-dated hires where the account exposes them |
| `employee_fields` | `id` | - | replace | Standard and custom employee-field metadata |
| `users` | `id` | - | replace | Enabled and disabled BambooHR user accounts |
| `locations` | `id` | - | replace | Active and archived job locations, including expanded state and country data; requires an OAuth token with the `field` scope |
| `time_off_requests` | `id` | `start` | merge | Time-off requests that overlap the requested date interval |
| `time_off_types` | `id` | - | replace | Available time-off categories |
| `time_off_default_hours` | `name` | - | replace | Default work hours by weekday |
| `time_off_policies` | `id` | - | replace | Non-deleted time-off policies |
| `timesheet_entries` | `id` | `date` | merge | Clock and hour entries from BambooHR Time Tracking |

`time_off_requests` uses `--interval-start` and `--interval-end` as an inclusive overlap
window. Without an interval it requests the full supported date range.

BambooHR only exposes the latest 365 days through its timesheet endpoint. Therefore,
`timesheet_entries` defaults to that entire available window and rejects dates outside it. Add
the company timezone to the URI, for example:

```sh
--source-uri 'bamboohr://acme?api_key=secret&timezone=America%2FDenver'
```

## Test accounts and demo data

BambooHR's [developer guide](https://documentation.bamboohr.com/docs/getting-started) recommends
developing against a test BambooHR account. The public trial is not a dependable self-service
API sandbox, so live connector validation requires an account controlled by the tester and
populated with non-production data in accordance with BambooHR's
[Developer Terms](https://www.bamboohr.com/legal/developer-terms-of-service).

For connector development without an account, ingestr's BambooHR tests run against a local fake
HTTP server with synthetic employees, locations, time-off records, and timesheet entries; no
customer data or credentials are stored in the repository.
