# Breezy HR
[Breezy HR](https://breezy.hr/) is an applicant tracking system for managing job positions, hiring pipelines, and candidates.

ingestr supports Breezy HR as a source.

## URI format

```
breezyhr://?api_key=<api_key>&company_id=<company_id>
```

URI parameters:
- `api_key`: a Breezy personal access token, starting with `breezy_pat_`.
- `company_id`: the ID of the Breezy company to load. It's the `_id` returned by `GET https://api.breezy.hr/v3/companies` for your token, not the company name or URL slug. To load several companies, run ingestr once per company.

Your Breezy plan must include API access. To create a token, click your profile icon in Breezy, open **My Settings**, select the **API Keys** tab, and click **Create API Key**. The token belongs to the user who creates it and sees the same data that user does, so create it as a company admin.

Here's a sample command that copies candidates from Breezy HR into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "breezyhr://?api_key=breezy_pat_123&company_id=abc123" \
  --source-table "candidates" \
  --dest-uri duckdb:///breezyhr.duckdb \
  --dest-table "public.candidates"
```

## Tables

Breezy HR source allows ingesting the following resources into separate tables:

| Table                    | PK                    | Inc Key        | Inc Strategy | Details                                                               |
| ------------------------ | --------------------- | -------------- | ------------ | --------------------------------------------------------------------- |
| departments              | company_id, id        | –              | replace      | Departments configured for each company                               |
| pipelines                | company_id, _id       | –              | replace      | Hiring pipelines and their stages                                     |
| questionnaires           | company_id, _id       | –              | replace      | Questionnaire definitions with their sections and questions           |
| custom_fields            | company_id, type, _id | –              | replace      | Candidate and position custom field definitions                       |
| positions                | _id                   | –              | replace      | Positions in every state; `state` shows whether each is open or closed |
| position_team_members    | position_id, _id      | –              | replace      | Hiring team members on each position and their roles                  |
| position_stream          | _id                   | `timestamp`    | delete+insert | Activity on each position, such as state changes and added candidates |
| position_custom_fields   | position_id, _id      | –              | replace      | Custom field values on each position                                  |
| candidates               | _id                   | –              | replace      | Full candidate profiles, one row per candidate per position           |
| candidate_stream         | candidate_id, _id     | –              | replace      | Candidate activity, such as stage moves, notes, and interviews        |
| candidate_scorecards     | candidate_id, _id     | –              | replace      | Scorecard ratings on each candidate                                   |
| candidate_questionnaires | candidate_id, _id     | –              | replace      | Questionnaires sent to each candidate, with their answers             |
| candidate_custom_fields  | candidate_id, _id     | –              | replace      | Custom field values on each candidate                                 |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

Every row has a `company_id` column. `position_team_members`, `position_stream`, `position_custom_fields` and `candidates` have a `position_id` column, and the other candidate tables have `position_id` and `candidate_id` columns. A person who applied to several positions appears once per position; `meta_id` on `candidates` identifies the person across positions.

## Incremental loading

When `--interval-start` / `--interval-end` are provided, `position_stream` replaces the events in that range: it deletes the table's rows whose `timestamp` falls in the range, then inserts the events Breezy returns for it, so events deleted in Breezy within the range, such as removed notes, are removed too. Besides position events, it also contains activity on the position's candidates, such as notes and stage moves. Events deleted before the range stay in the table, including those of deleted candidates, and a range with no events at all is skipped; run with `--full-refresh` to remove them. The other tables are fully refreshed on each run and ignore the interval. Candidate tables are refreshed in full because changes such as a new scorecard or custom field value don't update the candidate's `updated_date`, and a full refresh also removes deleted candidates. When no interval is provided, all records are fetched.
