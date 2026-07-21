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
| `user` | - | - | replace | Refers to an individual who has access to a Salesforce org or instance. |
| `user_role` | - | - | replace | A standard object that represents a role within the organization's hierarchy.|
| `opportunity` | id | last_timestamp | merge | Represents a sales opportunity for a specific account or contact. |
| `opportunity_line_item` | id | last_timestamp | merge | Represents individual line items or products associated with an Opportunity. |
| `opportunity_contact_role` | id | last_timestamp | merge | Represents the association between an Opportunity and a Contact. |
| `account` | id | last_timestamp | merge | Individual or organization that interacts with your business. |
| `contact`  | id | - | replace | An individual person associated with an account or organization. |
| `lead`  | id | - | replace | Prospective customer/individual/org. that has shown interest in a company's products/services. |
| `campaign`  | id | - | replace | Marketing initiative or project designed to achieve specific goals, such as generating leads. |
| `campaign_member`  | id | last_timestamp | merge  | Association between a Contact or Lead and a Campaign. |
|  `product`  | id | - | replace  | For managing and organizing your product-related data within the Salesforce ecosystem. |
|  `pricebook`   | id | - | replace  | Used to manage product pricing and create price books. |
|  `pricebook_entry`   | id | - | replace  | Represents a specific price for a product in a price book. |
|  `task`   | id | last_timestamp | merge | Used to track and manage various activities and tasks within the Salesforce platform.  |
|  `event`   | id | last_timestamp | merge | Used to track and manage calendar-based events, such as meetings, appointments, or calls. |
|  `custom:<custom_object_name>`   | - | - | replace | Track and store data that’s unique to your organization. For more information about custom objects in Salesforce, read [here](https://developer.salesforce.com/docs/atlas.en-us.object_reference.meta/object_reference/sforce_api_objects_custom_objects.htm)|

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
> Salesforce API limits may affect the frequency and volume of data ingestion. Incremental loading is supported for objects with the `SystemModstamp` field, but some objects may require full-refresh loads. This is indicated by `mode` in the tables above. Tables with mode `replace` don't support incremental loads, while the ones with `merge` do.

## Field-level security

ingestr ingests only the fields the authenticating user is allowed to read. It builds its query from Salesforce's `describe` response, and Salesforce omits any field the user lacks **Read** field-level security (FLS) on — with no error. Such fields simply arrive empty (NULL) in the destination, even though they hold data in Salesforce.

If a field you expect is unexpectedly empty, check that the user (or its permission set / profile) has Read access to it. You can confirm what the user can see by describing the object as that user, for example:

```plaintext
GET https://<your-domain>.my.salesforce.com/services/data/v59.0/sobjects/<object>/describe
```

Only the fields listed in the response are ingested. Grant Read FLS on the missing fields and re-run.
