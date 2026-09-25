# Salesforce
[Salesforce](https://www.salesforce.com/) is a cloud-based customer relationship management (CRM) platform that helps businesses manage sales, customer interactions, and business processes. It provides tools for sales automation, customer service, marketing, analytics, and application development.

Ingestr supports Salesforce as a source and as a [reverse-ETL destination](#salesforce-as-a-destination).

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

## Field-level security

ingestr ingests only the fields the authenticating user is permitted to read. Its query is built from what Salesforce returns for that user, so a field the user cannot read is not fetched.

If you want a field to be ingested, make sure the user (or its permission set or profile) has **Read** access to it, for example through its field-level security (FLS) settings. You can see which fields the user can read by describing the object as that user:

```plaintext
GET https://<your-domain>.my.salesforce.com/services/data/v59.0/sobjects/<object>/describe
```

Only the fields listed in the response are ingested. Grant Read access to the fields you want and run the ingestion again.

## Salesforce as a destination

ingestr can write rows from any source back into Salesforce (reverse ETL). Each source row creates, updates or deletes one Salesforce record.

### Quick start

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.customers" \
  --dest-uri "salesforce://?access_token=<token>&domain=<domain>" \
  --dest-table "Contact?external_id=External_Id__c" \
  --primary-key customer_id \
  --incremental-strategy merge
```

For every row, this finds the Contact whose `External_Id__c` equals the row's `customer_id`: if it exists it is updated, otherwise it is created. The other source columns (`FirstName`, `Email`, …) are written to the Contact fields of the same name.

The destination URI is the same as the [source URI](#uri-format), with the same login options. The user you connect with needs **Create**, **Edit** and, for `delete`/`replace`, **Delete** permission on the object, plus **Edit** access to every field you write.

### Objects and fields

- `--dest-table` is the object's **API name**: `Contact`, `Account`, `Opportunity`, or a custom object such as `Invoice__c`.
- Each source column is written to the field with the same **API name**, e.g. `FirstName` or `Amount__c`. Names are not case-sensitive.
- Find API names in Setup → **Object Manager**. Labels don't work:

| | Label | API name to use |
|---|---|---|
| Custom object | Invoice | `Invoice__c` |
| Custom field | Amount | `Amount__c` |
| Object from a managed package | Invoice (package `acme`) | `acme__Invoice__c` |

- ingestr doesn't create objects or fields. A source column that isn't a field, or is read-only (a formula, or a system field like `CreatedDate`), stops the run **before anything is written**. Drop such columns, for example with `--sql-exclude-columns`.
- A source column named `Id` is only used to find records, never written. ingestr's own `_ingestr_loaded_at` and `_ingestr_run_id` columns are never sent.

### Strategies

`--incremental-strategy` is **required**:

| Strategy | What it does | What it needs |
|---|---|---|
| `merge` | Updates the matching record, or creates it if there is none | `external_id=` and `--primary-key` |
| `update` | Updates matching records only; a row with no match is rejected | nothing when matching by record `Id` |
| `append` | Always creates new records | nothing |
| `delete` | Deletes matching records | nothing when matching by record `Id` |
| `replace` | Like `merge`, then deletes every record that isn't in the source | `external_id=` and `--primary-key` |

- **`append`** never matches, so re-running it creates duplicates, unless a unique field on the object refuses them (`DUPLICATE_VALUE`). `external_id=` isn't allowed with it; `--primary-key` is accepted and only names rejected rows in the report.
- **`delete`** moves records to the Recycle Bin, where they can be restored for 15 days. They still count toward the org's storage until the bin is emptied.

> [!WARNING]
> `replace` deletes **every** record of the object that isn't in your source, including ones created in Salesforce or by other tools. Use it only when your source is the complete list. As a safety net, a run with **0 source rows** deletes nothing, and with the default `--reject-mode fail` a run with any rejected row deletes nothing either.

### Matching records

Two settings decide which record a row updates or deletes:

| Setting | Set with | Example |
|---|---|---|
| The **Salesforce field** to match on | `external_id=` in `--dest-table` | `Contact?external_id=External_Id__c` |
| The **source column** that holds the value | `--primary-key` | `--primary-key customer_id` |

**`merge` and `replace`** need both. The field must be marked **External ID** in Salesforce, or be a standard lookup field such as a Contact's or Lead's `Email`. Standard objects have no External ID field by default, so create one first: Setup → Object Manager → *object* → Fields & Relationships → New, and tick **External ID**. If the field isn't unique and two records share a value, Salesforce rejects that row instead of guessing. The record `Id` can't be used, because Salesforce can't create a record with an id you choose.

**`update` and `delete`** match by the Salesforce record id by default, using a source column named `Id`, so a source that has the ids needs no extra flags:

```sh
# Delete the contacts listed in the source by their Salesforce record id
ingestr ingest \
  --source-uri "csv://churned.csv" \
  --source-table "churned" \
  --dest-uri "salesforce://?access_token=<token>&domain=<domain>" \
  --dest-table "Contact" \
  --incremental-strategy delete
```

They can also match on any other text, number or id field with `external_id=`, even one that isn't unique. Then **every** matching record is updated or deleted. For example, one row keyed on `Department` updates every contact in that department.

### Linking records

In Salesforce, a record points to its parent through a **lookup field** on itself. A Contact's Account, for example, is stored in the Contact's `AccountId` field. To link records, write that field like any other column, in one of two ways:

| You have | Column name | Example value |
|---|---|---|
| The parent's Salesforce record id | The lookup field, e.g. `AccountId` | `001WU00002EGrguYAD` |
| Your own key for the parent | `<Relationship>.<Field>`, e.g. `Account.Ext_Id__c` | `ACC-1` |

With your own key, Salesforce finds the parent for you:

- The **relationship** is usually the lookup field without `Id`: `AccountId` → `Account`, `OwnerId` → `Owner`, `ParentId` → `Parent`. For a custom lookup, replace `__c` with `__r`: `Parent__c` → `Parent__r`.
- The **field** must identify one parent: a field marked External ID, or a unique field such as a user's email (`Owner.Email`).
- The source column can have this name already, or you can map one with `--columns`, e.g. `--columns 'Account.Ext_Id__c::account_code'`.
- **Unlinking:** an empty (null) value removes the link. Pass `--write-nulls=false` to leave existing links alone.

**Lookups that can point to more than one object.** A Task's or Event's `Who` can be a Contact or a Lead, and its `What` an Account, Opportunity, Case and more. For these, put the object in the middle, `<Relationship>.<Object>.<Field>`:

```
Subject,Who.Contact.Ext_Id__c
Follow-up call,C-1
```

- A column without the object, such as `Who.Ext_Id__c`, stops the run before writing and lists the objects to choose from. This is about the column's final name, whether it comes straight from the source or from `--columns`.
- `Owner` is the exception: `Owner.Email` means a User, so no object is needed. For a Queue, write `Owner.Group.<Field>`.
- If rows point to different objects, use one column per object (`Who.Contact.Ext_Id__c` and `Who.Lead.Ext_Id__c`) and fill one per row. A row that fills both is rejected.
- A record id in the lookup field itself (`WhoId`) needs no object name.

**Many-to-many links.** Salesforce stores these as a record of their own, with a lookup to each side. For example, an `OpportunityContactRole` links a Contact to an Opportunity, and a `CampaignMember` links a Contact or Lead to a Campaign. Each source row creates one link, and can set the link's own fields:

```sh
# opportunity_contacts.csv:
#   Opportunity.Opp_Ext_Id__c,Contact.Ext_Id__c,Role,IsPrimary
#   OPP-7,C-1,Decision Maker,true
#   OPP-7,C-2,Evaluator,false
ingestr ingest \
  --source-uri "csv://opportunity_contacts.csv" \
  --source-table "opportunity_contacts" \
  --dest-uri "salesforce://?access_token=<token>&domain=<domain>" \
  --dest-table "OpportunityContactRole" \
  --incremental-strategy append
```

To remove links, `delete` those records. To keep the links exactly in step with your source, give the object an External ID field and use `replace`. This only works when Salesforce lets the link's lookups change: a `CampaignMember`'s Campaign, for example, is fixed once created, so the run stops before writing. For such links, `delete` the stale ones and `append` the new ones.

### Load method

Add `load_method` to the destination URI to choose how records are sent:

- **`bulk`** *(default)*: records are sent as a [Bulk API 2.0](https://developer.salesforce.com/docs/atlas.en-us.api_asynch.meta/api_asynch/bulk_api_2_0.htm) job, and the rejected rows are reported when the job finishes.
- **`rest`**: records are written while the source is read, and results come back right away.

```sh
--dest-uri "salesforce://?access_token=<token>&domain=<domain>&load_method=rest"
```

Bulk is the default because it uses only a few of the org's daily API calls per run, however many rows you send, and Salesforce processes the rows in parallel. REST uses one call per 200 rows, so a large table synced often can use up the org's daily limit.

Choose `rest` when:

- you need `--reject-mode fail_fast`, which only works with `rest` (a bulk job reports rejects only once it finishes);
- the syncs are small and frequent, and you want each run to finish right away (every bulk job waits in Salesforce's queue first, usually seconds, sometimes longer);
- you want records written as the source is read. With `bulk`, records are sent once the source has been read, so a source that fails part-way writes little or nothing.

Every strategy works the same with both. Bulk jobs are listed in Setup → **Bulk Data Load Jobs**.

### Reverse-ETL options

These flags only apply when Salesforce is the destination.

#### `--reject-mode`

What to do with a row Salesforce refuses, or that matches no record:

| Mode | Behaviour |
|---|---|
| `fail` *(default)* | Write every valid row, then fail the run and list the rejected rows |
| `fail_fast` | Stop at the first rejected row. Needs [`load_method=rest`](#load-method) |
| `skip` | Write every valid row, list the rejected rows, and succeed |

One bad row never blocks the others. Each rejected row is listed with Salesforce's reason (e.g. `REQUIRED_FIELD_MISSING`, `DUPLICATE_VALUE`) and its key. If a record is briefly locked because something else is editing it at the same moment, ingestr retries it a few times first.

Problems that affect the whole run, such as an expired login or a full org storage, always stop it, whatever the mode.

> [!NOTE]
> Salesforce writes can't be rolled back, so with `fail` or `fail_fast` some records may already be written when the run stops.

#### `--write-nulls`

- **`true`** *(default)*: an empty (null) source value clears the field in Salesforce.
- **`false`** (`--write-nulls=false`): an empty source value is skipped, and the field keeps its current value.

It has no effect on `delete`.

### Column mapping

If a source column's name differs from the Salesforce field, rename it with `--columns 'field::source_column'`. Separate several with commas:

```sh
--columns 'FirstName::first_name,External_Id__c::customer_id'
```

- Only renaming is allowed. Field types are set in Salesforce, so an entry with a type is rejected.
- `--primary-key` takes the **new** name. With `--columns 'Ext_Id__c::customer_ref'`, pass `--primary-key Ext_Id__c`.
- [Link columns](#linking-records) can be mapped the same way, including lookups that can point to more than one object: `--columns 'Account.Ext_Id__c::account_code,Who.Contact.Ext_Id__c::contact_key'`.
- Names are sent exactly as written. `--schema-naming` is ignored, since changing `FirstName` to `first_name` would no longer match the field.

### Values

Numbers, booleans, text, dates and timestamps are sent in the form Salesforce expects. Timestamps are sent in UTC; Salesforce keeps them to the whole second. Nested or JSON values are sent as a JSON string.
