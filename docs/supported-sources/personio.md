# Personio
[Personio](https://personio.de/) is a human resources management software that helps businesses
streamline HR processes, including recruitment, employee data management, and payroll, in one
platform.

Resources that can be loaded using this verified source are:
- `employees` : Retrieves company employees details
- `absences` : Retrieves absence periods for absences tracked in days
- `absences_types` : Retrieves list of various types of employee absences
- `attendances` : Retrieves attendance records for each employee
- `projects` : Retrieves a list of all company projects
- `document_categories` : Retrieves all document categories of the company
- `employees_absences_balance` : The transformer, retrieves the absence balance for a specific employee

## Initialize the pipeline

```bash
dlt init personio duckdb
```

## Setup verified source

To grab Personio credentials and configure the verified source, please refer to the
[full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/personio#grab-credentials)