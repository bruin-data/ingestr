# Asana
[Asana](https://asana.com/) is a software-as-a-service platform designed for team collaboration and work management. Teams can create projects, assign tasks, set deadlines, and communicate directly within Asana. It also includes reporting tools, file attachments, calendars, and goal tracking.

## URI format

The URI format for Asana is as follows:
```
asana://<workspace_id>?access_token=<access_token>
```

URI parameters:
- `workspace_id` is the `gid` of the workspace. 
- `access_token` is a personal access token.

You can obtain `workspace_id` by going to the [admin console](https://help.asana.com/s/article/how-to-access-the-admin-console). The URL in your browser will look something like this:

```
https://app.asana.com/admin/fake-123456789/
```

In this example `fake-123456789` is your workspace id.

## Setting up an Asana Integration

You can obtain a personal access token from the [developer console](https://app.asana.com/0/my-apps). For more information, see [Asana developers documentation](https://developers.asana.com/docs/personal-access-token).

## Example
Let's say you have a workspace with id `workspace-1337` and you want to ingest all tasks into a duckdb database called `work.db`. For this example the value of `access_token` will be `fake_token`

You can run the following to achieve this:
```sh
ingestr ingest \
  --source-uri "asana://workspace-1337?access_token=fake_token" \
  --source-table "tasks" \
  --dest-uri "duckdb://./work.db" \
  --dest-table "public.tasks"
```


## Tables

Asana source allows ingesting the following sources into separate tables:

| Table | Primary/Merge Key | Inc Key | Inc Strategy | Details |
|-------|----|----------|--------------|---------|
| `workspaces` | - | - | replace | Information about people, materials, or assets required to complete a task or project successfully. Full reload on each run. |
| `projects` | - | - | replace | Collections of tasks and related information. Full reload on each run. |
| `sections` | - | - | replace | Project sections and organization. Full reload on each run. |
| `tags` | - | - | replace | Labels that can be attached to tasks, projects, or conversations. Full reload on each run. |
| `tasks` | gid | modified_at | merge | Tasks within a project. Only tasks that belong to a project can be ingested. Uses modified_since API parameter for incremental loading. |
| `stories` | - | - | replace | Updates or comments that team members can add to a task or project. |
| `teams` | - | - | replace | Groups of individuals who work together to complete projects and tasks. Full reload on each run. |
| `users` | - | - | replace | Individuals who have access to the Asana platform. Full reload on each run. |


Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> Asana does not support incremental loading for many endpoints in its APIs, which means ingestr will load endpoints incrementally if they support it, and do a full-refresh if not.
