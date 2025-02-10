# Salesforce
[Salesforce](https://www.salesforce.com/) is a cloud-based customer relationship management (CRM) platform that helps businesses manage sales, customer interactions, and business processes. It provides tools for sales automation, customer service, marketing, analytics, and application development.

Ingestr supports Salesforce as a source.

## URI format

The URI format for Salesforce is as follows:
```
salesforce://?username=<username>&password=<password>&token=<token>
```

URI parameters:
- `username` is your Salesforce account username.
- `password` is your Salesforce account password.
- `token` is your Salesforce security token.

You can obtain your security token by logging into your Salesforce account and navigating to the user settings under "Reset My Security Token."

## Setting up a Salesforce Integration

You can obtain an OAuth access token by setting up a connected app in Salesforce and using OAuth 2.0 authentication. For more information, see [Salesforce API Authentication](https://developer.salesforce.com/docs/atlas.en-us.api_rest.meta/api_rest/quickstart_oauth.htm).

## Example

Let's say:
* Your Salesforce username is `user`.
* Your password is `password123`.
* Your security token is `fake_token`.
* You want to ingest `account` data from your salesforce account
* You want to save this data in a duckdb database `sf.db` under the table `public.account`

You can run the following command to achieve this:
```sh
ingestr ingest \
  --source-uri "salesforce://?username=user&password=password123&token=fake_token" \
  --source-table "account" \
  --dest-uri "duckdb:///sf.db" \
  --dest-table "public.account"
```

## Tables

Salesforce source allows ingesting the following objects into separate tables:

| **Table**                 | **Mode**   | **Description** |
|---------------------------|-----------|----------------|
| `user`                  | replace   | Refers to an individual who has access to a Salesforce org or instance. |
| `user_role`             | replace   | A standard object that represents a role within the organization's hierarchy. |
| `opportunity`          | merge     | Represents a sales opportunity for a specific account or contact. |
| `opportunity_line_item` | merge     | Represents individual line items or products associated with an Opportunity. |
| `opportunity_contact_role` | merge  | Represents the association between an Opportunity and a Contact. |
| `account`               | merge     | Individual or organization that interacts with your business. |
| `contact`               | replace   | An individual person associated with an account or organization. |
| `lead`                  | replace   | Prospective customer/individual/org. that has shown interest in a company's products/services. |
| `campaign`              | replace   | Marketing initiative or project designed to achieve specific goals, such as generating leads. |
| `campaign_member`       | merge     | Association between a Contact or Lead and a Campaign. |
| `product`               | replace   | For managing and organizing your product-related data within the Salesforce ecosystem. |
| `pricebook`             | replace   | Used to manage product pricing and create price books. |
| `pricebook_entry`      | replace   | Represents a specific price for a product in a price book. |
| `task`                 | merge     | Used to track and manage various activities and tasks within the Salesforce platform. |
| `event`                | merge     | Used to track and manage calendar-based events, such as meetings, appointments, or calls. |

Use these as `--source-table` parameters in the `ingestr ingest` command.

> [!WARNING]
> Salesforce API limits may affect the frequency and volume of data ingestion. Incremental loading is supported for objects with the `SystemModstamp` field, but some objects may require full-refresh loads. This is indicated by `mode` in the tables above. Tables with mode `replace` don't support incremental loads, while the ones with `merge` do.

