# Salesforce
[Salesforce](https://www.salesforce.com/) is a cloud-based customer relationship management (CRM) platform that helps businesses manage sales, customer interactions, and business processes. It provides tools for sales automation, customer service, marketing, analytics, and application development.

Ingestr supports Salesforce as a source.

## URI format

The URI format for Salesforce using an OAuth access token is as follows:

```plaintext
salesforce://?access_token=<access_token>&domain=<domain>
```

URI parameters:
- `access_token` is an OAuth access token for your Salesforce org.
- `domain` is your Salesforce My Domain, instance host, or full instance URL. For sandboxes, use the sandbox My Domain URL, for example `https://MyDomainName--SandboxName.sandbox.my.salesforce.com`.

You can also use username, password, and security token authentication:

```
salesforce://?username=<username>&password=<password>&token=<token>&domain=<domain>
```

URI parameters:
- `username` is your Salesforce account username.
- `password` is your Salesforce account password.
- `token` is your Salesforce security token. This is not the same as an OAuth access token.
- `domain` is your Salesforce instance domain (for example, `login`, `test`, or `your-domain.my`). You can also pass the full Salesforce host or URL.

To use the OAuth 2.0 client credentials flow, use the following URI:
```
salesforce://?grant_type=client_credentials&client_id=<client_id>&client_secret=<client_secret>&domain=<domain>
```

URI parameters:
- `grant_type=client_credentials` selects the client credentials flow. This is optional when both `client_id` and `client_secret` are provided.
- `client_id` is the consumer key for your Salesforce connected app.
- `client_secret` is the consumer secret for your Salesforce connected app.
- `domain` is your Salesforce My Domain or instance domain (for example, `your-domain.my`). You can also pass the full Salesforce host or URL.

You can obtain your security token by logging into your Salesforce account and navigating to the user settings under "Reset My Security Token."

## Setting up a Salesforce Integration

### Option A: Salesforce CLI access token

This is the most direct setup path when you want to authenticate interactively in a browser and then pass the resulting OAuth access token to ingestr.

1. Create a Salesforce developer org from [developer.salesforce.com/signup](https://developer.salesforce.com/signup) if you do not already have an org. Salesforce sends the org username by email; for developer orgs it can look like `your.original.email.3f6ksj33ew99@agentforce.com`.
2. Install the Salesforce CLI from the [Salesforce CLI setup guide](https://developer.salesforce.com/docs/atlas.en-us.262.0.sfdx_setup.meta/sfdx_setup/sfdx_setup_install_cli.htm).
3. Log in to your org:

   ```sh
   sf org login web
   ```

   For a sandbox or a specific My Domain URL, pass the instance URL:

   ```sh
   sf org login web --instance-url https://MyDomainName--SandboxName.sandbox.my.salesforce.com
   ```

4. Display the org details and note the `Instance Url` and username:

   ```sh
   sf org display --target-org <salesforce-username>
   ```

   Recent Salesforce CLI versions hide secrets from this command. If you see a warning that secrets are hidden, use the auth command in the next step instead of setting `SF_TEMP_SHOW_SECRETS=true`.

5. Show the access token:

   ```sh
   sf org auth show-access-token --target-org <salesforce-username>
   ```

6. Use the access token and instance URL in the ingestr source URI:

   ```sh
   ingestr ingest \
     --source-uri "salesforce://?access_token=<access_token>&domain=<instance-url>" \
     --source-table "account" \
     --dest-uri "duckdb:///sf.db" \
     --dest-table "public.account"
   ```

   URL-encode query parameter values if they contain special characters such as `&`, `+`, or `%`.

For Salesforce's official OAuth quickstart, see [Salesforce API Authentication](https://developer.salesforce.com/docs/atlas.en-us.api_rest.meta/api_rest/quickstart_oauth.htm). For developer org setup, see [Set Up Your Developer Environment](https://developer.salesforce.com/docs/atlas.en-us.262.0.api_rest.meta/api_rest/quickstart_dev_org.htm).

### Option B: Username, password, and security token

Use this option when you have a Salesforce security token from user settings. The `token` URI parameter is the Salesforce security token, not the OAuth access token printed by `sf org auth show-access-token`.

### Option C: Client credentials

Use this option when you have a connected app configured for the OAuth 2.0 client credentials flow. ingestr exchanges `client_id` and `client_secret` for an access token automatically.

## Example

Let's say:
* Your Salesforce access token is `fake_access_token`.
* Your Salesforce instance URL is `https://your-domain.my.salesforce.com`.
* You want to ingest `account` data from your salesforce account
* You want to save this data in a duckdb database `sf.db` under the table `public.account`

You can run the following command to achieve this:
```sh
ingestr ingest \
  --source-uri "salesforce://?access_token=fake_access_token&domain=https://your-domain.my.salesforce.com" \
  --source-table "account" \
  --dest-uri "duckdb:///sf.db" \
  --dest-table "public.account"
```

## Tables

Salesforce source allows ingesting the following objects into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|---------|--------------|---------|
| `account` | id | SystemModstamp | merge | Individual or organization that interacts with your business. |
| `account_history` | id | CreatedDate | merge | Tracks changes made to fields on Account records. |
| `agent_work` | id | SystemModstamp | merge | Represents a work item routed to an agent through Omni-Channel. |
| `campaign` | id | SystemModstamp | merge | Marketing initiative or project designed to achieve specific goals, such as generating leads. |
| `campaign_history` | id | CreatedDate | merge | Tracks changes made to fields on Campaign records. |
| `campaign_member` | id | SystemModstamp | merge | Association between a Contact or Lead and a Campaign. |
| `campaign_member_status` | id | - | replace | Represents the possible member statuses for a Campaign. |
| `case` | id | SystemModstamp | merge | A customer issue or problem, used for support and service tracking. |
| `case_feed` | id | SystemModstamp | merge | Feed items (posts, comments, updates) associated with a Case. |
| `case_history` | id | CreatedDate | merge | Tracks changes made to fields on Case records. |
| `case_milestone` | id | SystemModstamp | merge | Represents a milestone (required step) in an entitlement process on a Case. |
| `contact` | id | SystemModstamp | merge | An individual person associated with an account or organization. |
| `contact_history` | id | CreatedDate | merge | Tracks changes made to fields on Contact records. |
| `content_document` | id | SystemModstamp | merge | A document uploaded to a library in Salesforce Files or CRM Content. |
| `content_version` | id | SystemModstamp | merge | A specific version of a document in Salesforce Files or CRM Content. |
| `conversation` | id | LastModifiedDate | merge | Represents a conversation in messaging channels. |
| `conversation_entry` | id | SystemModstamp | merge | An individual message or event within a Conversation. |
| `conversation_participant` | id | LastModifiedDate | merge | A participant in a Conversation. |
| `dashboard` | id | - | replace | Represents a dashboard, a visual snapshot of source report data. |
| `dashboard_component` | id | - | replace | An individual component (chart, table, metric) on a Dashboard. |
| `email_message` | id | SystemModstamp | merge | An email in Salesforce, typically associated with a Case or other record. |
| `event` | id | SystemModstamp | merge | Used to track and manage calendar-based events, such as meetings, appointments, or calls. |
| `event_relation` | id | - | replace | Represents people (invitees) or resources related to an Event. |
| `feed_comment` | id | - | replace | A comment added to a feed item in Chatter. |
| `folder` | id | - | replace | A folder used to organize documents, dashboards, reports, or email templates. |
| `forecasting_quota` | id | - | replace | An individual user's or territory's forecast quota for a period. |
| `group` | id | - | replace | A set of users, such as a public group or queue. |
| `lead` | id | SystemModstamp | merge | Prospective customer/individual/org. that has shown interest in a company's products/services. |
| `lead_history` | id | CreatedDate | merge | Tracks changes made to fields on Lead records. |
| `opportunity` | id | SystemModstamp | merge | Represents a sales opportunity for a specific account or contact. |
| `opportunity_contact_role` | id | - | replace | Represents the association between an Opportunity and a Contact. |
| `opportunity_field_history` | id | CreatedDate | merge | Tracks changes made to fields on Opportunity records. |
| `opportunity_history` | id | CreatedDate | merge | Tracks stage and status changes on Opportunity records. |
| `opportunity_line_item` | id | SystemModstamp | merge | Represents individual line items or products associated with an Opportunity. |
| `opportunity_split` | id | - | replace | Represents credit split between team members on an Opportunity. |
| `opportunity_split_type` | id | - | replace | Represents the type of an Opportunity split, such as revenue or overlay. |
| `permission_set` | id | - | replace | A set of permissions and settings that can be assigned to users. |
| `permission_set_assignment` | id | - | replace | The assignment of a Permission Set to a user. |
| `pricebook` | id | SystemModstamp | merge | Used to manage product pricing and create price books. |
| `pricebook_entry` | id | SystemModstamp | merge | Represents a specific price for a product in a price book. |
| `product` | id | SystemModstamp | merge | For managing and organizing your product-related data within the Salesforce ecosystem. |
| `profile` | id | - | replace | Defines a user's permissions and access settings. |
| `record_type` | id | - | replace | Represents a record type, which offers different business processes and page layouts per object. |
| `report` | id | - | replace | Represents a report, a set of data returned in rows and columns. |
| `service_presence_status` | id | - | replace | A presence status that can be assigned to agents in Omni-Channel. |
| `survey_invitation` | id | - | replace | An invitation sent to a participant to complete a survey. |
| `survey_question_score` | id | - | replace | Aggregated score data for a survey question. |
| `survey_response` | id | - | replace | A participant's response to a survey. |
| `survey_subject` | id | - | replace | The association between a survey and another record. |
| `task` | id | SystemModstamp | merge | Used to track and manage various activities and tasks within the Salesforce platform. |
| `task_relation` | id | SystemModstamp | merge | Represents people or other records related to a Task. |
| `topic` | id | - | replace | A topic used to organize and discover content in Chatter. |
| `topic_assignment` | id | - | replace | The assignment of a Topic to a record or feed item. |
| `upgrades_history` | id | - | replace | Tracks changes made to fields on Upgrades records. |
| `user` | - | - | replace | Refers to an individual who has access to a Salesforce org or instance. |
| `user_history` | id | CreatedDate | merge | Tracks changes made to fields on User records. |
| `user_role` | - | - | replace | A standard object that represents a role within the organization's hierarchy. |
| `user_service_presence` | id | SystemModstamp | merge | Represents an agent's presence status in Omni-Channel, used for tracking availability. |
| `voice_call` | id | SystemModstamp | merge | Represents a phone call made or received through Service Cloud Voice. |
| `voice_call_feed` | id | SystemModstamp | merge | Feed items associated with a Voice Call. |
| `voice_call_recording` | id | SystemModstamp | merge | Represents the recording of a Voice Call. |
| `custom:<custom_object_name>` | - | - | replace | Track and store data that's unique to your organization. For more information about custom objects in Salesforce, read [here](https://developer.salesforce.com/docs/atlas.en-us.object_reference.meta/object_reference/sforce_api_objects_custom_objects.htm) |

Use these as `--source-table` parameters in the `ingestr ingest` command.

 ## Examples
 Copy user_role data from Salesforce into a DuckDB database:
```sh
ingestr ingest \
  --source-uri "salesforce://?access_token=<access_token>&domain=<instance-url>" \
  --source-table "user_role" \
  --dest-uri "duckdb:///sf.db" \
  --dest-table "public.user_role"
```

Copy account data using OAuth 2.0 client credentials:
```sh
ingestr ingest \
  --source-uri "salesforce://?grant_type=client_credentials&client_id=<client_id>&client_secret=<client_secret>&domain=<domain>" \
  --source-table "account" \
  --dest-uri "duckdb:///sf.db" \
  --dest-table "public.account"
```

Copy custom object data from Salesforce into a DuckDB database:
```sh
ingestr ingest \
  --source-uri "salesforce://?username=<username>&password=<password>&token=<token>&domain=<domain>" \
  --source-table "custom:My__Community_Group__c" \
  --dest-uri "duckdb:///sf.db" \
  --dest-table "public.my_community"
```

> [!WARNING]
> Salesforce API limits may affect the frequency and volume of data ingestion. Incremental loading is supported for objects with a timestamp field such as `SystemModstamp`, `CreatedDate`, or `LastModifiedDate`, but some objects require full-refresh loads. This is indicated by the strategy in the table above: tables with strategy `replace` don't support incremental loads, while the ones with `merge` do.