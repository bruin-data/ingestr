# Google Ads
[Google Ads](https://ads.google.com/), formerly known as Google Adwords, is an online advertising platform developed by Google, where advertisers bid to display brief advertisements, service offerings, product listings, and videos to web users. It can place ads in the results of search engines like Google Search (the Google Search Network), mobile apps, videos, and on non-search websites.

## URI format

The URI format for Google Ads is as follows:
```plaintext
googleads://<customer_id>?credentials_path=/path/to/service-account.json&dev_token=<dev_token>
```

URI parameters:

- `customer_id`: Customer ID of the Google Ads account to use.
- `credentials_path`: path to the service account JSON file.
- `dev_token`: [developer token](https://developers.google.com/google-ads/api/docs/get-started/dev-token) to use for accessing the account.

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

Let's say we want to ingest information about campaigns and ads that we've created on our Google Ads account, and save them to a table `public.campaigns` in duckdb database called `adverts.db`.

For this example, we'll assume that:
* The service account JSON file is located in the current directory and is named `svc_account.json`
* customer id is `1234567890`
* the developer token is `dev-token-spec-1`

You can run the following to achieve this:
```sh
ingestr ingest \
  --source-uri "googleads://12345678?credentials_path=./svc_account.json&dev_token=dev-token-spec-1" \
  --source-table "campaigns" \
  --dest-uri "duckdb://./adverts.db" \
  --dest-table "public.campaigns"
```

## Tables

| Name             | Description                                                             |
|------------------|-------------------------------------------------------------------------|
| customers        | Businesses or individuals who pay to advertise their products           |
| campaigns        | Structured sets of ad groups and advertisements                         |
| change_events    | Modifications made to an account's ads, campaigns, and related settings |
| customer_clients | Accounts that are managed by a given account                            |

> [!WARNING]
> Google Ads source doesn't support incremental loading. This means that ingestr will do a full-reload every time you run `ingest`.
