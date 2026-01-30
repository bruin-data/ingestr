mak # Customer.io

[Customer.io](https://customer.io/) is a customer engagement platform that enables businesses to send automated messages across email, push, SMS, and more.

ingestr supports Customer.io as a source.

## URI format

The URI format for Customer.io is as follows:

```plaintext
customerio://?api_key=<api-key>&region=<region>
```

URI parameters:

- `api_key`: The API key for authentication with the Customer.io API.
- `region`: The region of your Customer.io account. Must be either `us` (default) or `eu`.

The URI is used to connect to the Customer.io API for extracting data.

## Setting up a Customer.io Integration

To get your API key:

1. Log in to your Customer.io account
2. Go to **Account Settings** > **API Credentials**
3. Create a new **App API Key** with read permissions

Once you have your API key, here's a sample command that will copy the data from Customer.io into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'customerio://?api_key=your_api_key&region=us' \
  --source-table 'broadcasts' \
  --dest-uri duckdb:///customerio.duckdb \
  --dest-table 'customerio.broadcasts'
```

The result of this command will be a table in the `customerio.duckdb` database.

## Tables

Customer.io source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [activities](https://customer.io/docs/api/app/#operation/listActivities) | id | – | replace | Retrieves account activity log. |
| [broadcasts](https://customer.io/docs/api/app/#operation/listBroadcasts) | id | updated | merge | Retrieves broadcast campaigns. |
| [broadcast_actions](https://customer.io/docs/api/app/#operation/listBroadcastActions) | id | updated | merge | Retrieves actions for broadcasts. |
| broadcast_action_metrics:period | broadcast_id, action_id, period, step_index | – | replace | Retrieves metrics for broadcast actions. Period: `hours`, `days`, `weeks`, `months`. |
| [broadcast_messages](https://customer.io/docs/api/app/#operation/listBroadcastMessages) | id | – | merge | Retrieves messages sent by broadcasts. |
| broadcast_metrics:period | broadcast_id, period, step_index | – | replace | Retrieves metrics for all broadcasts. Period: `hours`, `days`, `weeks`, `months`. |
| [campaigns](https://customer.io/docs/api/app/#operation/listCampaigns) | id | updated | merge | Retrieves triggered campaigns. |
| [campaign_actions](https://customer.io/docs/api/app/#operation/listCampaignActions) | id | updated | merge | Retrieves actions for campaigns. |
| campaign_action_metrics:period | campaign_id, action_id, period, step_index | – | replace | Retrieves metrics for campaign actions. Period: `hours`, `days`, `weeks`, `months`. |
| [campaign_messages](https://customer.io/docs/api/app/#operation/getCampaignMessages) | id | – | merge | Retrieves messages/deliveries sent from campaigns. |
| campaign_metrics:period | campaign_id, period, step_index | – | replace | Retrieves metrics for all campaigns. Period: `hours`, `days`, `weeks`, `months`. |
| [collections](https://customer.io/docs/api/app/#operation/listCollections) | id | updated_at | merge | Retrieves data collections. |
| [customers](https://customer.io/docs/api/app/#operation/getPeopleFilter) | cio_id | – | replace | Retrieves all customers/people in the workspace. |
| [customer_activities](https://customer.io/docs/api/app/#operation/getPersonActivities) | id | – | replace | Retrieves activities performed by each customer. |
| [customer_attributes](https://customer.io/docs/api/app/#operation/getPersonAttributes) | customer_id | – | replace | Retrieves attributes for each customer. |
| [customer_messages](https://customer.io/docs/api/app/#operation/getPersonMessages) | id | – | merge | Retrieves messages sent to each customer. |
| [customer_relationships](https://customer.io/docs/api/app/#operation/getPersonRelationships) | customer_id, object_type_id, object_id | – | replace | Retrieves object relationships for each customer. |
| [exports](https://customer.io/docs/api/app/#operation/listExports) | id | updated_at | merge | Retrieves export jobs. |
| [info_ip_addresses](https://customer.io/docs/api/app/#operation/listIPAddresses) | ip | – | replace | Retrieves IP addresses used by Customer.io. |
| [messages](https://customer.io/docs/api/app/#operation/listMessages) | id | – | merge | Retrieves sent messages. |
| [newsletters](https://customer.io/docs/api/app/#operation/listNewsletters) | id | updated | merge | Retrieves newsletters. |
| newsletter_metrics:period | newsletter_id, period, step_index | – | replace | Retrieves metrics for all newsletters. Period: `hours`, `days`, `weeks`, `months`. |
| [newsletter_test_groups](https://customer.io/docs/api/app/#operation/listNewsletterTestGroups) | id | – | replace | Retrieves test groups for newsletters. |
| [object_types](https://customer.io/docs/api/app/#operation/getObjectTypes) | id | – | replace | Retrieves object types in the workspace. |
| [objects](https://customer.io/docs/api/app/#operation/getObjectsFilter) | object_type_id, object_id | – | replace | Retrieves all objects for each object type. |
| [reporting_webhooks](https://customer.io/docs/api/app/#operation/listReportingWebhooks) | id | – | replace | Retrieves reporting webhooks. |
| [segments](https://customer.io/docs/api/app/#operation/listSegments) | id | updated_at | merge | Retrieves customer segments. |
| [sender_identities](https://customer.io/docs/api/app/#operation/listSenderIdentities) | id | – | replace | Retrieves sender identities. |
| [subscription_topics](https://customer.io/docs/api/app/#operation/getTopics) | id | – | replace | Retrieves subscription topics. |
| [transactional_messages](https://customer.io/docs/api/app/#operation/listTransactional) | id | – | replace | Retrieves transactional message templates. |
| [workspaces](https://customer.io/docs/api/app/#operation/listWorkspaces) | id | – | replace | Retrieves workspaces in your account. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Examples

### Metrics Tables

Metrics tables require a period suffix. Use the format `table_name:period` where period can be `hours`, `days`, `weeks`, or `months`.

```sh
# Get daily broadcast metrics
ingestr ingest \
  --source-uri 'customerio://?api_key=your_api_key&region=us' \
  --source-table 'broadcast_metrics:days' \
  --dest-uri duckdb:///customerio.duckdb \
  --dest-table 'customerio.broadcast_metrics'

# Get hourly campaign metrics
ingestr ingest \
  --source-uri 'customerio://?api_key=your_api_key&region=us' \
  --source-table 'campaign_metrics:hours' \
  --dest-uri duckdb:///customerio.duckdb \
  --dest-table 'customerio.campaign_metrics'
```

### People and Objects

```sh
# Get all customers with their identifiers
ingestr ingest \
  --source-uri 'customerio://?api_key=your_api_key&region=us' \
  --source-table 'customers' \
  --dest-uri duckdb:///customerio.duckdb \
  --dest-table 'customerio.customers'

# Get detailed customer attributes
ingestr ingest \
  --source-uri 'customerio://?api_key=your_api_key&region=us' \
  --source-table 'customer_attributes' \
  --dest-uri duckdb:///customerio.duckdb \
  --dest-table 'customerio.customer_attributes'
```

## Incremental Loading

Customer.io supports incremental loading for tables that have an `updated` or `updated_at` field. When using the `--interval-start` and `--interval-end` flags, ingestr will only fetch records that have been updated within the specified time range.

```sh
ingestr ingest \
  --source-uri 'customerio://?api_key=your_api_key&region=us' \
  --source-table 'broadcasts' \
  --dest-uri duckdb:///customerio.duckdb \
  --dest-table 'customerio.broadcasts' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```
