# Mailchimp

[Mailchimp](https://mailchimp.com/) is an all-in-one marketing platform that helps businesses manage and talk to their clients, customers, and other interested parties through email marketing campaigns, automated messages, and targeted ads.

ingestr supports Mailchimp as a source.

## URI format

The URI format for Mailchimp is as follows:

```plaintext
mailchimp://?api_key=<api-key>&server=<server-prefix>
```

URI parameters:

- `api_key`: The API key used for authentication with the Mailchimp API
- `server`: The server prefix for your Mailchimp account (e.g., `us10`, `us19`)

The URI is used to connect to the Mailchimp API for extracting data.

## Setting up a Mailchimp Integration

To get your Mailchimp API credentials:

1. Log in to your Mailchimp account
2. Navigate to **Account** → **Extras** → **API keys**
3. Create a new API key or use an existing one
4. Note your server prefix from your API key (the part after the dash, e.g., `us10` from `xxxxx-us10`)

Once you have your credentials, here's a sample command that will copy the data from Mailchimp into a DuckDB database:

```sh
ingestr ingest --source-uri 'mailchimp://?api_key=your_api_key&server=us10' --source-table 'campaigns' --dest-uri duckdb:///mailchimp.duckdb --dest-table 'mailchimp.campaigns'
```

The result of this command will be a table in the `mailchimp.duckdb` database.

## Tables

Mailchimp source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [account](https://mailchimp.com/developer/marketing/api/root/) | - | - | replace | Retrieves account information including company details, account tier, and contact information. |
| [account_exports](https://mailchimp.com/developer/marketing/api/account-exports/) | - | - | replace | Retrieves account export information. |
| [audiences](https://mailchimp.com/developer/marketing/api/lists/) | id | date_created | merge | Retrieves audience (list) information including subscriber counts and list settings. |
| [authorized_apps](https://mailchimp.com/developer/marketing/api/authorized-apps/) | id | - | replace | Retrieves third-party applications authorized to access your account. |
| [automations](https://mailchimp.com/developer/marketing/api/automations/) | id | create_time | merge | Retrieves automated email workflows and their configurations. |
| [batches](https://mailchimp.com/developer/marketing/api/batches/) | - | - | replace | Retrieves batch operation status and results. |
| [campaign_folders](https://mailchimp.com/developer/marketing/api/campaign-folders/) | id | - | replace | Retrieves folders used to organize campaigns. |
| [campaigns](https://mailchimp.com/developer/marketing/api/campaigns/) | id | create_time | merge | Retrieves email campaigns including their content, settings, and metadata. |
| [chimp_chatter](https://mailchimp.com/developer/marketing/api/activity-feed/) | - | - | replace | Retrieves recent activity feed from your Mailchimp account. |
| [connected_sites](https://mailchimp.com/developer/marketing/api/connected-sites/) | id | updated_at | merge | Retrieves websites connected to your Mailchimp account. |
| [conversations](https://mailchimp.com/developer/marketing/api/conversations/) | id | last_message.timestamp | merge | Retrieves conversation threads from connected channels. |
| [ecommerce_stores](https://mailchimp.com/developer/marketing/api/ecommerce-stores/) | id | updated_at | merge | Retrieves e-commerce store information including products and orders. |
| [facebook_ads](https://mailchimp.com/developer/marketing/api/facebook-ads/) | id | updated_at | merge | Retrieves Facebook ad campaigns managed through Mailchimp. |
| [landing_pages](https://mailchimp.com/developer/marketing/api/landing-pages/) | id | updated_at | merge | Retrieves landing pages created in Mailchimp. |
| lists_activity | - | - | replace | Retrieves recent activity for list members. Includes `audiences_id` reference. |
| lists_clients | - | - | replace | Retrieves email clients used by list members. Includes `audiences_id` reference. |
| lists_growth_history | - | - | replace | Retrieves historical growth data for the list. Includes `audiences_id` reference. |
| lists_interest_categories | - | - | replace | Retrieves interest categories (groups) for the list. Includes `audiences_id` reference. |
| lists_locations | - | - | replace | Retrieves geographic locations of list members. Includes `audiences_id` reference. |
| lists_merge_fields | - | - | replace | Retrieves custom merge fields defined for the list. Includes `audiences_id` reference. |
| lists_segments | - | - | replace | Retrieves segments (filtered subsets) of the list. Includes `audiences_id` reference. |
| [reports](https://mailchimp.com/developer/marketing/api/reports/) | id | send_time | merge | Retrieves campaign performance reports and analytics. |
| reports_advice | - | - | replace | Retrieves feedback and suggestions for improving campaign performance. Includes `reports_id` reference. |
| reports_domain_performance | - | - | replace | Retrieves email performance broken down by email domain. Includes `reports_id` reference. |
| reports_locations | - | - | replace | Retrieves geographic location data for campaign opens. Includes `reports_id` reference. |
| reports_sent_to | - | - | replace | Retrieves list of recipients who were sent the campaign. Includes `reports_id` reference. |
| reports_sub_reports | - | - | replace | Retrieves sub-reports for A/B test campaigns. Includes `reports_id` reference. |
| reports_unsubscribed | - | - | replace | Retrieves list of members who unsubscribed from the campaign. Includes `reports_id` reference. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Examples

### Ingesting Campaign Data

```sh
ingestr ingest \
  --source-uri 'mailchimp://?api_key=your_api_key&server=us10' \
  --source-table 'campaigns' \
  --dest-uri duckdb:///mailchimp.duckdb \
  --dest-table 'mailchimp.campaigns'
```



The `reports_advice` table will include a `reports_id` column that references the parent campaign report.


The `lists_segments` table will include an `audiences_id` column that references the parent audience/list.

## Notes

> [!NOTE]
> Nested resources (e.g., `reports_advice`, `lists_segments`) automatically include a reference to their parent resource through an ID column (`reports_id` or `audiences_id`).

