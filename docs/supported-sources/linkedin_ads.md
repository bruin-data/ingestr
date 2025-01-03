# LinkedIn Ads
LinkedIn Ads is a platform that allows businesses and marketers to create, manage, and analyze advertising campaigns.

Ingestr supports LinkedIn Ads as a source.

## URI format
The URI format for LinkedIn Ads as a source is as follows:

```plaintext
linkedinads://?access_token=<access_token>&account_ids=<account_ids>&time_granularity=<time_granularity>"
```
## URI parameters:
- `access_token`(required): Used for authentication and is necessary to access reports through the LinkedIn Ads API.
- `account_ids`(required): The comma-separated list of account IDs to retrieve data for.

[LinkedIn Ads](https://learn.microsoft.com/en-us/linkedin/marketing/integrations/ads-reporting/ads-reporting?view=li-lms-2024-11&tabs=http#analytics-finder) requires an `access_token` and `account_ids` to retrieve reports from the LinkedIn Ads API. Please follow the following steps or https://learn.microsoft.com/en-us/linkedin/marketing/quick-start:

###Create a LinkedIn developer application
- Log in to LinkedIn with a developer account.
- Navigate to the Apps page and click the Create App icon. Fill in the fields below:
  - For App Name, enter a name.
  - For LinkedIn Page, enter your company's name or LinkedIn Company Page URL.
  - For Privacy policy URL, enter the link to your company's privacy policy.
  - For App logo, upload your company's logo.
Check I have read and agree to these terms, then click Create App. LinkedIn redirects you to a page showing the details of your application.
You can verify your app using the following steps:

Click the Settings tab. On the App Settings section, click Verify under Company. A popup window will be displayed. To generate the verification URL, click on Generate URL, then copy and send the URL to the Page Admin (this may be you). Click on I'm done. If you are the administrator of your Page, simply run the URL in a new tab (if not, an administrator will have to do the next step). Click on Verify.

To display the Products page, click the Product tab. For Marketing Developer Platform, click Request access. A popup window will be displayed. Review and Select I have read and agree to these terms. Finally, click Request access.

###Authorize the app to access your LinkedIn Ads data.
To authorize your application, click the Auth tab. Copy the Client ID and Client Secret. In the Oauth 2.0 settings, provide a redirect URL for your app.

Click the OAuth 2.0 tools link in the Understanding authentication and OAuth 2.0 section on the right side of the page.

Click Create token.

Select the scopes you want to use for your app. We recommend using the following scopes:
r_ads
r_ads_reporting

Click Request access token. You will be redirected to an authorization page. Use your LinkedIn credentials to log in and authorize your app and obtain your Access Token and Refresh Token. Access token is expired in 2 months.If either of your tokens expire, you can generate new ones by returning to LinkedIn's Token Generator. 

See the LinkedIn docs to locate these [IDs](https://www.linkedin.com/help/linkedin/answer/a424270/find-linkedin-ads-account-details?lang=en).

## Table: Custom Reports    
Custom reports allow you to retrieve data based on specific dimension and metrics.

Custom Table Format:
```
custom:<dimension>:<metrics>
```
### Parameters:
- `dimension`(required): A comma-separated list of [dimensions]. Must be `campaign`, `account`, `creative` and from time dimension [date or month].
- `metrics`(required): A comma-separated list of [metrics](https://learn.microsoft.com/en-us/linkedin/marketing/integrations/ads-reporting/ads-reporting?view=li-lms-2024-11&tabs=http#metrics-available) to retrieve.
 

> [!NOTE]
> By default, Ingestr fetches data from January 1, 2018 to the today date. You can specify a custom date range using the `start-interval` and `end-interval` parameters.

### Example

Retrieve data for campaign with `account_ids` id_123 and id_456:
```sh
ingestr ingest \                         
    --source-uri "linkedinads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'custom:campaign,date:impressions,clicks' \
    --dest-uri 'duckdb:///linkedin.duckdb' \
    --dest-table 'dest.campaign'
```

The applied parameters for the report are:
- dimension: `campaign,date`
- metrics: `impressions`, `clicks`

Retrieve data for creative with `account_ids` id_123 and id_456:
```sh
ingestr ingest \                         
    --source-uri "linkedinads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'custom:creative,month:likes,impressions' \
    --dest-uri 'duckdb:///linkedin.duckdb' \
    --dest-table 'dest.creative'
    --start-interval '2024-10-15'
    --end-interval '2024-12-31'
```
The applied parameters for the report are:
- dimension: `creative,month`
- metrics: `likes`, `impressions`

```sh
ingestr ingest \                         
    --source-uri "linkedinads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'custom:account,month:shares,totalEngagements,impressions,' \
    --dest-uri 'duckdb:///linkedin.duckdb' \
    --dest-table 'dest.account'
```
The applied parameters for the report are:
- dimension: `account,month`
- metrics: `shares`, `totalEngagements`, `impressions`

This command will retrieve data and save it to the destination table in the DuckDB database.

<img alt="linkedin_ads_img" src="../media/linkedin_ads.png" />