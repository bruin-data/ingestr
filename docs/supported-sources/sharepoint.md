# SharePoint

[SharePoint Online](https://www.microsoft.com/microsoft-365/sharepoint/collaboration) is Microsoft's document management and collaboration platform. ingestr can read files (Excel and CSV) from a SharePoint document library through the [Microsoft Graph API](https://learn.microsoft.com/graph/overview).

Each file (or glob of files) is landed as raw rows. Cell values are kept as text and a few extra columns are added to record each row's source file, sheet, and position.

## URI format

One connection corresponds to one SharePoint **site**:

```
sharepoint://?tenant_id=<id>&client_id=<id>&client_secret=<secret>&hostname=<host>&site=<site_path>
```

URI parameters:

- `tenant_id`: Azure AD tenant ID of the app registration
- `client_id`: app registration (service principal) client ID
- `client_secret`: app registration client secret
- `hostname`: SharePoint tenant hostname, e.g. `example.sharepoint.com`
- `site`: server-relative site path, e.g. `sites/Example`
- `library` (optional): document library (drive) name; defaults to the site's default **Documents** library. Set it to read from a secondary library on the same site.

Authentication uses the OAuth2 **client-credentials** flow. The app registration needs application permission to read the site's files (e.g. `Sites.Read.All` or `Files.Read.All`), granted with admin consent.

To read files from a different site (a different document library), define a second connection pointing at that `site`.

## Source table format

The source table identifies a file (or a glob of files) within the site's default document library, optionally followed by hints:

```
<path>#<format>,<key>=<value>,<key>=<value>
```

- `<path>` is the file path relative to the document library root. It may contain spaces and `&`, and may use glob wildcards: `*`, `**` (recursive), `?`, `[...]`, `{a,b}`.
- The string is split on the **last** `#` only when the part after it parses as a valid hint list; otherwise the whole string is treated as the path. Use `%23` to embed a literal `#` in a path.
- Hints are comma-separated. A bare token is a format override; everything else is `key=value`:

| Hint | Applies to | Meaning |
|---|---|---|
| `xlsx` / `csv` | all | format override (otherwise detected from the extension) |
| `sheet=<name>` | Excel | read a single sheet |
| `sheets=<a>\|<b>\|<c>` | Excel | read and stack several sheets (separated by `\|`) |
| `skip=<n>` | all | drop `n` rows before the header row |
| `drop_empty` | all | skip rows whose data columns are all empty (off by default) |
| `date_cols=<a>\|<b>` | Excel | column names whose Excel serial values are converted to ISO date strings (separated by `\|`) |
| `raw` / `formatted` | Excel | raw values (default): unformatted numbers (dates as serial numbers unless listed in `date_cols`); or the cell's displayed/formatted text |
| `encoding=<enc>` | CSV | input encoding, e.g. `utf-16le` |
| `sep=<sep>` | CSV | field separator; `tab` (or `\t`) for tab-delimited |

> [!NOTE]
> Sheet names referenced in `sheet=`/`sheets=` cannot contain `|`, `,`, `=`, or `#`.

Examples:

```
Reports/products.xlsx#xlsx,sheet=Sheet1
Reports/parameters.xlsx#sheet=Forecast,skip=4
Reports/regional data.xlsx#sheets=North|South|East|West
Reports/monthly/*.xlsx#sheets=Jan|Feb|Mar
Reports/export.csv#csv,encoding=utf-16le,sep=tab
```

## Added columns

Every row is stamped with:

| Column | Meaning |
|---|---|
| `_source_file` | the file path the row came from — distinguishes files in a glob |
| `_sheet_name` | the Excel sheet tab the row came from (null for CSV) |
| `_row_idx` | 0-based row position within the sheet/file, in read order (after `skip` and the header) |

`(_sheet_name, _row_idx)` is unique within a file; add `_source_file` to make it unique across a glob. Use ingestr's `--columns` exclusion to drop any of these if you don't need them.

> [!NOTE]
> For Excel, a blank row inside the data range keeps its `_row_idx` slot (it lands as an all-empty row, or leaves a gap when `drop_empty` is set), so the original row ordering is preserved. For CSV, completely blank physical lines are skipped by the parser and do not occupy a `_row_idx` — only rows with at least one field (including all-empty rows like `,,`) are counted.

## Reading the data

Values are read as text. For Excel, `raw` (the default) keeps the underlying cell value (e.g. `1234.5`) rather than its display formatting (e.g. `1,234.50`); pass `formatted` to get the displayed text instead. Empty cells are emitted as empty strings, and merged cells keep their value in the top-left cell only.

Excel stores dates as serial numbers. In `raw` mode (the default) a date cell therefore lands as its serial (e.g. `45306`) — readable date strings are opt-in via the `date_cols` hint, which names the columns to convert. Listed columns have their serial values turned into ISO strings by value: `2024-01-15` for a whole-number serial, `2024-01-15 13:30:00` when there is a fractional (time) part, and `13:30:00` for a fraction-only serial. Non-numeric cells in those columns (e.g. text dates) are left untouched. Columns not listed in `date_cols` keep their raw serials. In `formatted` mode the cell's own display format is used and `date_cols` has no effect.

> [!NOTE]
> `date_cols` names are matched **case-insensitively** against the landed column names (after blank/duplicate header normalization), and conversion is by value — it does not read cell number formats, which keeps reads fully streaming and low-memory on large workbooks.

By default no rows are filtered — blank/spacer rows are landed as-is (matching the raw read). The `drop_empty` hint skips rows where every data column is empty; `_row_idx` retains each row's true position, so dropped rows leave gaps and the original row ordering is preserved.

If no sheet is specified, the first sheet is read. A requested sheet (via `sheet=` or `sheets=`) that does not exist is an error that lists the available sheet names — sheet names are case-sensitive.

## Combining sheets and files

When several sheets (`sheets=`) or several files (a glob) are read into one table, columns are matched **by name** and unioned: new columns are appended in the order first seen, and rows from a sheet/file that lack a column get `NULL` there. Sheets/files do not need identical columns.

## Examples

Copy a single sheet into DuckDB:

```sh
ingestr ingest \
  --source-uri 'sharepoint://?tenant_id=...&client_id=...&client_secret=...&hostname=<tenant>.sharepoint.com&site=sites/<name>' \
  --source-table 'Reports/products.xlsx#sheet=Sheet1' \
  --dest-uri duckdb:///sharepoint.duckdb \
  --dest-table 'raw.products'
```

Stack every workbook in a folder, unioning three sheets each:

```sh
ingestr ingest \
  --source-uri 'sharepoint://?tenant_id=...&client_id=...&client_secret=...&hostname=<tenant>.sharepoint.com&site=sites/<name>' \
  --source-table 'Reports/monthly/*.xlsx#sheets=Jan|Feb|Mar' \
  --dest-uri duckdb:///sharepoint.duckdb \
  --dest-table 'raw.monthly'
```

## Write strategies

The source honors `--incremental-strategy`, defaulting to `replace`. `append`, `merge`, and `delete+insert` are also selectable.

> [!CAUTION]
> SharePoint does not support incremental extraction — every run reads the entire file (or glob). With `append` this duplicates rows on each run by design; use `replace` (the default) unless you have a reason to accumulate.
