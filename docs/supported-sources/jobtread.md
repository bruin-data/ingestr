# JobTread
[JobTread](https://www.jobtread.com/) is a construction management platform that helps contractors manage jobs, estimates, invoices, budgets, tasks, and more.

ingestr supports JobTread as a source.

## URI format

```
jobtread://?grant_key=<grant_key>&organization_id=<organization_id>
```

URI parameters:
- `grant_key`: A grant key used to authenticate with the JobTread API.
- `organization_id`: The ID of the organization to ingest data from.

To create a grant key, navigate to the [grant management page](https://app.jobtread.com/settings/integrations/api/grants) in your JobTread account. Upon creation, the grant key will be displayed one time so make sure to copy it.

To find your organization ID, run the following query in the [API Explorer](https://app.jobtread.com/docs):
```
currentGrant:
  user:
    memberships:
      nodes:
        organization:
          id: {}
          name: {}
```

Once you have both, here's a sample command that will copy the data from JobTread into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "jobtread://?grant_key=your_grant_key&organization_id=your_org_id" \
  --source-table "jobs" \
  --dest-uri duckdb:///jobtread.duckdb \
  --dest-table "public.jobs"
```

## Tables

JobTread source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| accounts | id | - | replace | Customer and vendor accounts |
| jobs | id | - | replace | Construction jobs/projects |
| contacts | id | - | replace | Contacts associated with accounts |
| documents | id | - | replace | All document types including estimates, invoices, bills, and orders |
| tasks | id | - | replace | Tasks and to-dos linked to jobs |
| cost_codes | id | - | replace | Budget cost code categories |
| cost_types | id | - | replace | Cost type definitions (labor, materials, etc.) |
| cost_items | id | - | replace | Budget line items on jobs |
| locations | id | - | replace | Job site locations with addresses |
| custom_fields | id | - | replace | Custom field definitions |
| daily_logs | id | - | replace | Daily job site logs with weather data |
| time_entries | id | - | replace | Time tracking records for labor |
| files | id | - | replace | File attachments |
| comments | id | - | replace | Comments on jobs, tasks, documents, etc. |
| document_payments | id | - | replace | Payments applied to documents |
| cost_groups | id | - | replace | Budget categories and templates |
| events | id | createdAt | merge | Audit log of all actions in the system |

Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> JobTread does not expose an `updatedAt` field on any entity, so most tables use a full replace strategy. Only the `events` table supports incremental loading via `createdAt` since events are immutable.

> [!WARNING]
> Grant keys expire after 3 months of inactivity.
