# Typeform

[Typeform](https://www.typeform.com/) is an online form and survey platform for building interactive forms and collecting responses.

ingestr supports Typeform as a source.

## URI format

```
typeform://?token=<personal_access_token>
```

URI parameters:

- `token`: The personal access token used to authenticate with the Typeform API.
- `region` (optional): The account region. Must be `us` (default) or `eu`.

For EU accounts, specify the region:

```
typeform://?token=<personal_access_token>&region=eu
```

## Setting up a Typeform Integration

Typeform requires a personal access token to connect to its API. To create one:

1. Log in to your Typeform account.
2. Go to your [personal tokens settings](https://admin.typeform.com/user/tokens).
3. Click **Generate a new token**, give it a name, and grant at least read access to forms, responses, workspaces, and themes.
4. Copy the generated token.

Once you have the token, here's a sample command that will copy form data into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "typeform://?token=your_token_here" \
  --source-table "forms" \
  --dest-uri duckdb:///typeform.duckdb \
  --dest-table "public.forms"
```

## Tables

Typeform source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [forms](https://www.typeform.com/developers/create/reference/retrieve-forms/) | id | last_updated_at | merge | All forms in the account with metadata (title, settings, theme, timestamps) |
| [responses](https://www.typeform.com/developers/responses/reference/retrieve-responses/) | response_id | submitted_at | merge | Submitted responses with answers and metadata, collected per form |
| [workspaces](https://www.typeform.com/developers/create/reference/retrieve-workspaces/) | id | - | replace | Workspaces in the account |
| [themes](https://www.typeform.com/developers/create/reference/retrieve-themes/) | id | - | replace | Themes available in the account |

Use these as the `--source-table` parameter in the `ingestr ingest` command.
