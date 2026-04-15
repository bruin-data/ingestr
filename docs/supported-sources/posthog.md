# PostHog

[PostHog](https://posthog.com/) is an open-source product analytics platform that helps teams understand user behavior with features like event tracking, feature flags, session recordings, and more.

ingestr supports PostHog as a source.

## URI format

The URI format for PostHog is:

```
posthog://?personal_api_key=<personal_api_key>&project_id=<project_id>
```

URI parameters:
- `personal_api_key`: The Personal API Key used for authentication with PostHog.
- `project_id`: The ID of your PostHog project, found in Project Settings.
- `base_url` (optional): The PostHog instance URL. Defaults to `https://us.posthog.com`. Use `https://eu.posthog.com` for the EU cloud.

The URI is used to connect to the PostHog API for extracting data.

## Setting up a PostHog integration

PostHog requires a Personal API Key to authenticate API requests. You can generate one from your PostHog dashboard:

1. Navigate to **Project Settings** > **Personal API Keys**.
2. Click **Create Personal API Key** and give it a descriptive name.
3. Copy the generated key.
4. Find your **Project ID** in **Project Settings**.

After completing the setup, you will have your `personal_api_key` and `project_id`. For example, if your personal API key is `phx_xxx` and project ID is `12345`, you can use the following command to copy data from PostHog into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "posthog://?personal_api_key=phx_xxx&project_id=12345" \
  --source-table "events" \
  --dest-uri duckdb:///posthog.duckdb \
  --dest-table "posthog.events"
```

If you are using the EU cloud instance, specify the `base_url` parameter:

```sh
ingestr ingest \
  --source-uri "posthog://?personal_api_key=phx_xxx&project_id=12345&base_url=https://eu.posthog.com" \
  --source-table "events" \
  --dest-uri duckdb:///posthog.duckdb \
  --dest-table "posthog.events"
```

## Available Tables

The PostHog source allows you to ingest the following tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| persons | id | last_seen_at | merge | People/users tracked in your project |
| feature_flags | id | updated_at | merge | Feature flags configuration |
| events | id | timestamp | append | Raw event data, supports server-side filtering with `after`/`before` parameters |
| cohorts | id | last_calculation | merge | User cohorts defined in your project |
| event_definitions | id | last_updated_at | merge | Event type definitions |
| property_definitions:event | id | updated_at | merge | Event property definitions |
| property_definitions:person | id | updated_at | merge | Person property definitions |
| property_definitions:session | id | updated_at | merge | Session property definitions |
| annotations | id | updated_at | merge | Project annotations |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

> [!NOTE]
> The `property_definitions` table requires a sub-type suffix (`:event`, `:person`, or `:session`) to specify which type of property definitions to ingest.
