# Docebo

[Docebo](https://www.docebo.com/) is a cloud-based Learning Management System (LMS) platform that provides enterprise learning solutions for employee training, customer education, and partner enablement.

ingestr supports Docebo as a source.

## URI format

The URI format for Docebo is as follows:

```plaintext
docebo://?base_url=<base_url>&client_id=<client_id>&client_secret=<client_secret>&username=<username>&password=<password>
```

URI parameters:
- `base_url`: the base URL of your Docebo instance (e.g., `https://yourcompany.docebosaas.com`)
- `client_id`: OAuth2 client ID for API authentication
- `client_secret`: OAuth2 client secret for API authentication
- `username`: (optional) username for password grant type authentication
- `password`: (optional) password for password grant type authentication

## Setting up a Docebo Integration

To obtain your Docebo API credentials:

1. Log in to your Docebo platform as a Super Admin
2. Navigate to **Settings** → **API and SSO**
3. Create a new OAuth2 application
4. Note the Client ID and Client Secret
5. Configure the appropriate scopes for your integration needs

You can use either:
- **Client Credentials Grant**: Use only `client_id` and `client_secret` (recommended for server-to-server integrations)
- **Password Grant**: Include `username` and `password` along with client credentials (for user-specific access)

Here's a sample command that will copy data from Docebo into a DuckDB database:

```bash
ingestr ingest \
--source-uri 'docebo://?base_url=https://yourcompany.docebosaas.com&client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'users' \
--dest-uri duckdb:///docebo.duckdb \
--dest-table 'dest.users'
```

## Tables

Docebo source supports ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|---------|--------------|---------|
| [branches](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Organizational units/branches in the org chart. Full reload on each run. |
| [categories](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Course categories for organizing content. Full reload on each run. |
| [certifications](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Certification programs and their configurations. Full reload on each run. |
| [course_enrollments](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | All course enrollment records with completion status. Full reload on each run. |
| [course_fields](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Custom course field definitions. Full reload on each run. |
| [course_learning_objects](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Learning objects (modules) within all courses. Full reload on each run. |
| [courses](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | All courses in the platform including e-learning, ILT, and webinars. Full reload on each run. |
| [external_training](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | External training records tracked in Docebo. Full reload on each run. |
| [group_members](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Membership records for all groups. Full reload on each run. |
| [groups](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | User groups/audiences for organizing learners. Full reload on each run. |
| [learning_plan_course_enrollments](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Course enrollments within learning plans. Full reload on each run. |
| [learning_plan_enrollments](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | User enrollments in learning plans. Full reload on each run. |
| [learning_plans](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Learning plans (learning paths) that group courses. Full reload on each run. |
| [sessions](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | ILT/classroom sessions for instructor-led courses. Full reload on each run. |
| [user_fields](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | Custom user field definitions. Full reload on each run. |
| [users](https://help.docebo.com/hc/en-us/articles/360019499600) | - | - | replace | All platform users including learners, instructors, and administrators. Full reload on each run. |

Use the table name as the `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> Docebo does not currently support incremental loading, which means ingestr will do a full-refresh on each run.

> [!NOTE]
> Date fields containing invalid dates (e.g., '0000-00-00') are automatically normalized to Unix epoch (1970-01-01) for compatibility.