# Personio
[Personio](https://personio.de/) is a human resources management software that helps businesses
streamline HR processes, including recruitment, employee data management, and payroll, in one
platform.

ingestr supports Personio as a source.

## URI format

The URI format for Personio is as follows:

```plaintext
personio://?client_id=<client-id>&client_secret=<client-secret>
```

URI parameters:

- `client_id`: the client ID used for authentication with the Personio API
- `client_secret`: the client secret used for authentication with the Personio API

## Setting up a Personio Integration

To grab personio credentials, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/personio#grab-credentials).

Once you complete the guide, you should have a client ID and client secret. Let's say your `client_id` is `id_123` and your `client_secret` is `secret_123`, here's a sample command that will copy the data from Personio into a DuckDB database:

```bash
ingestr ingest --source-uri 'personio://?client_id=id_123&client_secret=secret_123' \
 --source-table 'employees' \
 --dest-uri duckdb:///personio.duckdb \
 --dest-table 'dest.employees'
```

Personio source allows ingesting the following resources into separate tables:
- `employees` : Retrieves company employees details
- `absences` : Retrieves absence periods for absences tracked in days
- `absence_types` : Retrieves list of various types of employee absences
- `attendances` : Retrieves attendance records for each employee
- `projects` : Retrieves a list of all company projects
- `document_categories` : Retrieves all document categories of the company
- `employees_absences_balance` : Retrieves the absence balance for a specific employee
- `custom_reports_list` : Retrieves a list of all custom reports
- `custom_reports` : Retrieves data from a custom report

Use these as `--source-table` parameter in the `ingestr ingest` command.