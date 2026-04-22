# SurveyMonkey

[SurveyMonkey](https://www.surveymonkey.com/) is an online survey platform that allows users to create surveys, collect responses, and analyze data.

ingestr supports SurveyMonkey as a source.

## URI format

```
surveymonkey://?access_token=<access_token>
```

URI parameters:

- `access_token`: The access token used to authenticate with the SurveyMonkey API.
- `datacenter` (optional): The datacenter region. Must be `us` (default), `eu`, or `ca`.

For EU or CA accounts, specify the datacenter:

```
surveymonkey://?access_token=<access_token>&datacenter=eu
```

## Setting up a SurveyMonkey Integration

SurveyMonkey requires an access token to connect to the API. To get one:

1. Go to the [SurveyMonkey Developer Portal](https://developer.surveymonkey.com/apps/) and create a new app.
2. Select "Public App" as the app type.
3. Under the Scopes section, enable the required scopes: **View Surveys**, **View Responses**, **View Response Details**, **View Collectors**, **View Contacts**.
4. Click **Update Scopes**.
5. Copy the **Access Token** from the Credentials section.

> [!NOTE]
> Draft app tokens expire after 90 days. For EU accounts, use the [EU Developer Portal](https://developer.eu.surveymonkey.com/apps/) and set `datacenter=eu` in the URI.

Once you have the access token, here's a sample command that will copy survey data into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "surveymonkey://?access_token=your_token_here" \
  --source-table "surveys" \
  --dest-uri duckdb:///surveymonkey.duckdb \
  --dest-table "public.surveys"
```

## Tables

SurveyMonkey source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [surveys](https://api.surveymonkey.com/v3/docs?shell#api-endpoints-get-surveys) | id | date_modified | merge | List of all surveys with metadata (title, dates, response count, question count) |
| [survey_details](https://api.surveymonkey.com/v3/docs?shell#api-endpoints-get-surveys-id-details) | id | date_modified | merge | Full survey details including nested pages and questions as JSON |
| [survey_responses](https://api.surveymonkey.com/v3/docs?shell#api-endpoints-survey-responses) | id | date_modified | merge | Survey response data with answers, collected per survey |
| [collectors](https://api.surveymonkey.com/v3/docs?shell#api-endpoints-get-surveys-survey_id-collectors) | id | date_modified | merge | Survey distribution channels (weblink, email, etc.) |
| [contact_lists](https://api.surveymonkey.com/v3/docs?shell#api-endpoints-get-contact_lists) | id | - | replace | Contact lists |
| [contacts](https://api.surveymonkey.com/v3/docs?shell#api-endpoints-get-contacts) | id | - | replace | Contacts across all statuses (active, optout, bounced) |

Use these as the `--source-table` parameter in the `ingestr ingest` command.