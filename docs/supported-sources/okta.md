# Okta
[Okta](https://www.okta.com/) is an identity and access management platform for managing users, groups, applications, and authentication policies.

ingestr supports Okta as a source.

## URI format

```
okta://<your-org>.okta.com?api_key=<api_token>
```

URI parameters:
- `<your-org>.okta.com`: your Okta org domain (for example `dev-123456.okta.com` or `mycompany.okta.com`).
- `api_key`: an Okta API token used to authenticate requests.

Create an API token in the Okta Admin Console under **Security → API → Tokens → Create Token**. The token inherits the permissions of the admin who creates it, so use an account that can read the resources you want to ingest.

Once you have the domain and token, here's a sample command that copies users from Okta into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "okta://dev-123456.okta.com?api_key=00aBcD..." \
  --source-table "users" \
  --dest-uri duckdb:///okta.duckdb \
  --dest-table "public.users"
```

## Tables

Okta source allows ingesting the following resources into separate tables:

| Table                | PK                 | Inc Key       | Inc Strategy | Details                                                                    |
| -------------------- | ------------------ | ------------- | ------------ | -------------------------------------------------------------------------- |
| users                | id                 | `lastUpdated` | merge        | All users in the org                                                       |
| groups               | id                 | `lastUpdated` | merge        | All groups in the org                                                      |
| group_members        | group_id, id       | –             | replace      | Members of each group (one row per user per group); full snapshot each run |
| applications         | id                 | `lastUpdated` | merge        | All applications configured in the org                                     |
| application_users    | app_id, id         | –             | replace      | Users assigned to each application; full snapshot each run                 |
| application_groups   | app_id, id         | –             | replace      | Groups assigned to each application; full snapshot each run                |
| system_log_events    | uuid               | `published`   | merge        | System log events (Okta retains roughly the last 90 days)                  |
| devices              | id                 | `lastUpdated` | merge        | Devices enrolled in the org                                                |
| policies             | id                 | `lastUpdated` | merge        | Policies across all policy types                                           |
| policy_rules         | id                 | `lastUpdated` | merge        | Rules belonging to each policy                                             |
| roles                | id                 | –             | replace      | Custom admin role definitions                                              |

Nested objects (such as a user's `profile` and `credentials`) are preserved as JSON columns.

### Incremental loading

Tables with an incremental key support incremental loading with the `--interval-start` and `--interval-end` flags. When no interval is provided, ingestr performs a full load. The System Log can only be backfilled as far as Okta's retention window (about 90 days).
