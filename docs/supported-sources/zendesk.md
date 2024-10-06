# Zendesk

[Zendesk](https://www.zendesk.com/) is a cloud-based customer service and support platform. It offers a range of features including ticket management, self-service options, knowledgebase management, live chat, customer analytics, and conversations.

ingestr supports Zendesk as a source.

The Zendesk supports two authentication methods:

- OAuth Token
- API Token

All resources supports API Token except "chats resources"

## URI Format

The URI format for Zendesk is as follows:

```plaintext
zendesk://<sub-domain>?oauth_token=<oauth_token>
```

```plaintext
zendesk://<sub-domain>?api_token=<api_token>&email=<email>
```

URI parameters:

- `subdomain`: Unique zendesk subdomain that can be found in account url
- `email`: Email address of the user
- `api_token`: API token used for authentication with zendesk
- `oauth_token` : OAuth token used for authentication with zendesk

## Setting up a Zendesk Integration

Zendesk requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk#setup-guide).

Once you complete the guide, if you decide to use an OAuth Token, you should have a subdomain and an OAuth token. Let’s say your subdomain is `mycompany` and your OAuth token is `qVsbdiasVt`.

```sh
ingestr ingest --source-uri "zendesk://subdomain=mycompany?oauth_token=qVsbdiasVt" \
--source-table 'tickets' \
--dest-uri 'duckdb:///zendesk.duckdb' \
--dest-table 'zendesk.tickets' \
--interval-start '2024-01-01'
```

If you decide to use an API Token, you should have a subdomain, email, and API token. Let’s say your subdomain is `mycompany`, your email is `john@get.com`, and your API token is `nbs123`.

```sh
ingestr ingest --source-uri "zendesk://subdomain=mycompany?email=john@get.com&api_token=nbs123" \
--source-table 'tickets' \
--dest-uri 'duckdb:///zendesk.duckdb' \
--dest-table 'zendesk.tickets' \
--interval-start '2024-01-01'
```

The result of this command will be a table in the `zendesk.duckdb` database

## Available Tables

Zendesk source allows ingesting the following sources into separate tables:

- `tickets`: Retrieves all tickets, which are the means through which customers communicate with agents.
- `ticket_metrics`: Retrieves various metrics about one or more tickets.
- `ticket_metric_events`: Retrives ticket metric events that occurred on or after the start time.
- `ticket_forms`: Retrieves all ticket forms.
- `organizations` :Retrieves organizations (your customers can be grouped into organizations by their email domain)
- `groups`: Retrieves groups of support agents.
- `sla_policies`: Retrives different sla policies.
- `targets`: Retrives targets where as targets are data from Zendesk to external applications like Slack when a ticket is updated or created.
- `activities`: Retrieves ticket activities affecting the agent -`automations`:Retrives automations for the current account.
- `brands`: Returns all brands for your account.
- `greetings`: Retrives all default or customs greetings.
- `settings`: Retrieves account settings related to Zendesk voice accounts.
- `addresses`: Retrives addresses information.
- `legs_incremental`: Retrieves detailed information about each agent involved in a call.
- `users`: Retrieves all users
- `calls`: Retrives all calls specific to channels.
- `chats`: Retrives available chats.
- `phone_numbers`:Retrieves all available phone numbers
- `lines`:Retrieves all available lines (phone numbers and digital lines) in your Zendesk voice account.
- `agents_activity`: Retrieves activity information for agents

Use these as `--source-table` parameter in the `ingestr ingest` command.
