# Zendesk

[Zendesk](https://www.zendesk.com/) is a cloud-based customer service and support platform. It offers a range of features including ticket management, self-service options, knowledge-base management, live chat, customer analytics, and conversations.

ingestr supports Zendesk as a source.

The Zendesk supports two authentication methods when connecting through ingestr:
- OAuth Token 
- API Token

For all resources except chat resources, you can use either the [API Token](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk#grab-zendesk-support-api-token) or the Zendesk Support [OAuth Token](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk#zendesk-support-oauth-token) to fetch data. However, for chat resources, you must use the [OAuth Token](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk#zendesk-chat) specific to Zendesk Chat.

## URI format

The URI format for Zendesk based on the authentication method:
### For OAuth token authentication:
```plaintext
zendesk://:<oauth_token>@<sub-domain>
```
### For API token authentication:
```plaintext
zendesk://<email>:<api_token>@<sub-domain>
```

URI parameters:

- `subdomain`: the unique Zendesk subdomain that can be found in the account URL. For example, if your account URL is `https://my_company.zendesk.com/`, then `my_company` is your subdomain
- `email`: the email address of the user
- `api_token`: the API token used for authentication with Zendesk
- `oauth_token`: the OAuth token used for authentication with Zendesk

## Setting up a Zendesk Integration

Zendesk requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk#setup-guide).

Once you complete the guide, if you decide to use an OAuth token, you should have a subdomain and an OAuth token. Let’s say your subdomain is `mycompany` and your OAuth token is `qVsbdiasVt`.

```sh
ingestr ingest --source-uri "zendesk://:qVsbdiasVt@mycompany" \
--source-table 'tickets' \
--dest-uri 'duckdb:///zendesk.duckdb' \
--dest-table 'dest.tickets' \
--interval-start '2024-01-01'
```

If you decide to use an API Token, you should have a subdomain, email, and API token. Let’s say your subdomain is `mycompany`, your email is `john@get.com`, and your API token is `nbs123`.

```sh
ingestr ingest --source-uri "zendesk://john@get.com:nbs123@mycompany" \
--source-table 'tickets' \
--dest-uri 'duckdb:///zendesk.duckdb' \
--dest-table 'dest.tickets' \
--interval-start '2024-01-01'
```

The result of this command will be a table in the `zendesk.duckdb` database.

## Tables

Zendesk source allows ingesting the following sources into separate tables:


| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [tickets](https://developer.zendesk.com/api-reference/ticketing/tickets/tickets/)      | id | updated_at | merge            |  Retrieves all tickets, which are the means through which customers communicate with agents |
| [ticket_metrics](https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_metrics/) | - | – | replace               | Retrieves various metrics about one or more tickets. |
| [ticket_metric_events](https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_metric_events/) | id | time | append               | Retrieves ticket metric events that occurred on or after the start time |
| [ticket_forms](https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_forms/) | | - | replace | Retrieves all ticket forms |
| [users](https://developer.zendesk.com/api-reference/ticketing/users/users/)         | - | – | replace               | Retrieves all users |
| [groups](https://developer.zendesk.com/api-reference/ticketing/groups/groups/)       | - | – | replace               | Retrieves groups of support agents |
| [organizations](https://developer.zendesk.com/api-reference/ticketing/organizations/organizations/) | - | – | replace               | Retrieves organizations |
| [brands](https://developer.zendesk.com/api-reference/ticketing/account-configuration/brands/)       | - | – | replace               | Retrieves all brands for your account |
| [sla_policies](https://developer.zendesk.com/api-reference/ticketing/business-rules/sla_policies/)  | - | – | replace               | Retrieves different SLA policies. |
| [activities](https://developer.zendesk.com/api-reference/ticketing/tickets/activity_stream/)  | - | – | replace               | Retrieves ticket activities affecting the agent. |
| [automations](https://developer.zendesk.com/api-reference/ticketing/business-rules/automations/)   | - | – | replace               | Retrieves the automations for the current account |
| [targets](https://developer.zendesk.com/api-reference/ticketing/targets/targets/)       | - | – | replace               | Retrieves targets where as targets are data from Zendesk to external applications like Slack when a ticket is updated or created. |
| [calls](https://developer.zendesk.com/api-reference/voice/talk-api/incremental_exports/#incremental-calls-export)        | id | updated_at | merge               | Retrieves all calls specific to channels |
| [addresses](https://developer.zendesk.com/api-reference/voice/talk-api/addresses/)     | - | – | replace               | Retrieves addresses information|
| [greetings](https://developer.zendesk.com/api-reference/voice/talk-api/greetings/)     | - | – | replace               | Retrieves all default or customs greetings |
| [phone_numbers](https://developer.zendesk.com/api-reference/voice/talk-api/phone_numbers/) | - | – | replace               | Retrieves all available phone numbers. |
| [settings](https://developer.zendesk.com/api-reference/voice/talk-api/voice_settings/)      | - | – | replace               | Retrieves account settings related to Zendesk voice accounts |
| [lines](https://developer.zendesk.com/api-reference/voice/talk-api/lines/)        | - | – | replace               | Retrieves all available lines, such as phone numbers and digital lines, in your Zendesk voice account. |
| [agents_activity](https://developer.zendesk.com/api-reference/voice/talk-api/stats/#list-agents-activity) | - | – | replace               | Retrieves activity information for agents |
| [legs_incremental](https://developer.zendesk.com/api-reference/voice/talk-api/incremental_exports/#incremental-call-legs-export) | id | updated_at | merge               | Retrieves detailed information about each agent involved in a call. |
| [chats](https://developer.zendesk.com/api-reference/live-chat/chat-api/incremental_export/)  | id | update_timestamp/ updated_timestamp | merge  | Retrieves available chats. |

Use these as `--source-table` parameter in the `ingestr ingest` command.
