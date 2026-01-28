# Google Ads
[Google Ads](https://ads.google.com/), formerly known as Google Adwords, is an online advertising platform developed by Google, where advertisers bid to display brief advertisements, service offerings, product listings, and videos to web users. It can place ads in the results of search engines like Google Search (the Google Search Network), mobile apps, videos, and on non-search websites.

## URI format

The URI format for Google Ads is as follows:
```plaintext
googleads://<customer_id>?credentials_path=/path/to/service-account.json&dev_token=<dev_token>
```

URI parameters:

- `customer_id`: Customer ID of the Google Ads account to use. You can specify multiple customer IDs separated by commas (e.g., `1234567890,0987654321,1122334455`).
- `credentials_path`: path to the service account JSON file.
- `dev_token`: [developer token](https://developers.google.com/google-ads/api/docs/get-started/dev-token) to use for accessing the account.
- `login_customer_id` (optional): The Manager Account (MCC) ID to use when accessing client accounts. Required when your service account has access to an MCC and you want to pull data from a client account under that MCC. See [Google Ads API docs](https://developers.google.com/google-ads/api/docs/concepts/call-structure#cid) for more details.

> [!NOTE]
> You may specify credentials using `credentials_base64` instead of `credentials_path`.
> The value of this parameter is the base64 encoded contents of the 
> service account json file. However, we don't recommend using this
> parameter, unless you're integrating ingestr into another system.
## Setting up a Google Ads integration

### Prerequisites
* A Google cloud [service account](https://cloud.google.com/iam/docs/service-account-overview)
* A Google Ads [developer token](https://developers.google.com/google-ads/api/docs/get-started/dev-token)
* A Google Ads account 


### Obtaining necessary credentials

You can use the [Google Cloud IAM Console](https://cloud.google.com/security/products/iam) to create a service account for ingesting data from Google Ads. Make sure to enable Google Ads API in your console.

Next, you need to add your service account user to your Google Ads account. See [Google Developers Docs](https://developers.google.com/google-ads/api/docs/oauth/service-accounts) for exact steps.

Finally, you need to obtain a Google Ads Developer Token. Developer token lets your app connect to the Google Ads API. Each developer token is assigned an API access level which controls the number of API calls you can make per day with as well as the environment to which you can make calls. See [Google Ads docs](https://developers.google.com/google-ads/api/docs/get-started/dev-token) for more information on how to obtain this token.

You also need the 10-digit customer id of the account you're making API calls to. This is displayed in the Google Ads web interface in the form 123-456-7890. In this case, your customer id would be `1234567890`

### Example

Let's say we want to ingest information about campaigns (on a daily interval) and save them to a table `public.campaigns` in duckdb database called `adverts.db`.

For this example, we'll assume that:
* The service account JSON file is located in the current directory and is named `svc_account.json`
* customer id is `1234567890`
* the developer token is `dev-token-spec-1`

You can run the following to achieve this:
```sh
ingestr ingest \
  --source-uri "googleads://12345678?credentials_path=./svc_account.json&dev_token=dev-token-spec-1" \
  --source-table "campaign_report_daily" \
  --dest-uri "duckdb://./adverts.db" \
  --dest-table "public.campaigns"
```

### Using a Manager Account (MCC)

If your service account has access to a Manager Account (MCC) and you want to pull data from a client account under that MCC, you need to specify the `login_customer_id` parameter:

```sh
ingestr ingest \
  --source-uri "googleads://CLIENT_ID?credentials_path=./svc_account.json&dev_token=dev-token-spec-1&login_customer_id=MCC_ID" \
  --source-table "campaign_report_daily" \
  --dest-uri "duckdb://./adverts.db" \
  --dest-table "public.campaigns"
```

Where:
- `CLIENT_ID` is the customer ID of the client account you want to pull data from
- `MCC_ID` is the customer ID of the Manager Account that has access to the client account

### Multiple Customer IDs

You can ingest data from multiple Google Ads accounts in a single command by specifying comma-separated customer IDs:

```sh
ingestr ingest \
  --source-uri "googleads://1234567890,0987654321,1122334455?credentials_path=./svc_account.json&dev_token=dev-token-spec-1" \
  --source-table "campaign_report_daily" \
  --dest-uri "duckdb://./adverts.db" \
  --dest-table "public.campaigns"
```

When using multiple customer IDs, each row in the output will include a `customer_id` field to identify which account the data came from.

### Overriding Customer IDs per Table

You can override the customer IDs from the URI by appending `:customer_ids` to the table name:

```sh
ingestr ingest \
  --source-uri "googleads://default_customer?credentials_path=./svc_account.json&dev_token=dev-token-spec-1" \
  --source-table "campaign_report_daily:1234567890,0987654321" \
  --dest-uri "duckdb://./adverts.db" \
  --dest-table "public.campaigns"
```

This will use `1234567890` and `0987654321` instead of `default_customer` from the URI.

## Tables

| Name             | Description                                                             |
|------------------|-------------------------------------------------------------------------|
| `account_report_daily` | Provides daily metrics aggregated at the account level. |
| `campaign_report_daily` | Provides daily metrics aggregated at the campaign level. |
| `ad_group_report_daily` | Provides daily metrics aggregated at the ad group level. |
| `ad_report_daily` | Provides daily metrics aggregated at the ad level. |
| `audience_report_daily` | Provides daily metrics aggregated at the audience level. |
| `keyword_report_daily` | Provides daily metrics aggregated at the keyword level. |
| `click_report_daily` | Provides daily metrics on clicks. |
| `landing_page_report_daily` | Provides daily metrics on landing page performance. |
| `search_keyword_report_daily` | Provides daily metrics on search keywords. |
| `search_term_report_daily` | Provides daily metrics on search terms. |
| `lead_form_submission_data_report_daily` | Provides daily metrics on lead form submissions. |
| `local_services_lead_report_daily` | Provides daily metrics on local services leads. |
| `local_services_lead_conversations_report_daily` | Provides daily metrics on local services lead conversations. |

## Custom Reports
`googleads` source supports custom reports. You can pass a custom report definition to `--source-table` and it will dynamically create a report for you. These reports are aggregated at a daily interval.

The format of a custom report looks like the following:
```
daily:{resource_name}:{dimensions}:{metrics}:{customer_ids}
```
Where:
* `{resource_name}` is a [Google Ads Resource](https://developers.google.com/google-ads/api/fields/v18/overview_query_builder#list-of-all-resources).
* `{dimensions}` is a comma separated list of the Resource's attribute fields, or fields of [attributed resources](https://developers.google.com/google-ads/api/docs/query/overview).
* `{metrics}` is a comma separated list of the Resource's [metrics](https://developers.google.com/google-ads/api/fields/v18/metrics). Note that the `metrics.` prefix is optional.
* `{customer_ids}` (optional) is a comma separated list of customer IDs to use for this report. If not provided, the customer IDs from the URI will be used.

Notes:
* `{dimensions}` and `{metrics}` are required.
* `segments` are currently not supported as dimensions.
* `segments.date` is automatically added to all custom reports.

### Custom Report Example
For this example, we will ingest data from `ad_group_ad_asset_view`.
We want to obtain the following info:
**dimensions**
  * ad_group.id
  * campagin.id
  * customer.id
**metrics**
  * metrics.clicks
  * metrics.conversions
  * metrics.impressions

To achieve this, we pass a `daily` report specification to `ingestr` source table as follows:
```sh
ingestr ingest \
  --source-uri "googleads://12345678?credentials_path=./svc_account.json&dev_token=dev-token-spec-1" \
  --source-table "daily:ad_group_ad_asset_view:ad_group.id,campaign.id,customer.id:clicks,conversions,impressions" \
  --dest-uri "duckdb:///custom.db" \
  --dest-table "public.report"
```

Notice the lack of `metrics.` prefix in the metrics segment. Please note that `--dest-table` is mandatory when creating
a custom report.

**With Custom Customer IDs**

You can specify customer IDs directly in the custom report definition. This overrides the customer IDs from the URI:
```sh
ingestr ingest \
  --source-uri "googleads://default_customer?credentials_path=./svc_account.json&dev_token=dev-token-spec-1" \
  --source-table "daily:ad_group_ad_asset_view:ad_group.id,campaign.id,customer.id:clicks,conversions,impressions:1234567890,0987654321" \
  --dest-uri "duckdb:///custom.db" \
  --dest-table "public.report"
```

In this example, `1234567890` and `0987654321` will be used instead of `default_customer` from the URI.
