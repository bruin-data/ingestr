# Apple App Store 
The [App Store](https://appstore.com/) is an app marketplace developed and maintained by Apple, for mobile apps on its iOS and iPadOS operating systems. The store allows users to browse and download approved apps developed within Apple's iOS SDK. Apps can be downloaded on the iPhone, iPod Touch, or iPad, and some can be transferred to the Apple Watch smartwatch or 4th-generation or newer Apple TVs as extensions of iPhone apps.

`ingestr` allows you to ingest analytics, sales and performance data using the [Apple App Store Connect API](https://developer.apple.com/documentation/appstoreconnectapi)

> [!NOTE]
> Sometimes, the data in App Store Analytics reports isn’t fully complete when first provided. This happens because some information takes longer to process and appears in the reports later. For example, certain usage or sales details might be updated after the initial report is generated to correct errors or include missing data. This means that the report for a certain date may include data points from older dates. `ingestr` takes care of updating these rows to show the updated values. However, caution should be exercised when analysing current date's data, as it maybe subject to change in the future. 
> see [Data Completeness and Corrections](https://developer.apple.com/documentation/analytics-reports/data-completeness-corrections) for more information.
## URI Format

The URI format for App Store is as follows:
```
appstore://?key_path=</path/to/key>&key_id=<key_id>&issuer_id=<issuer_id>&app_id=<app_id>
```

URI Parameters:
* `key_path`: path to API private key
* `key_id`: ID of the generated key
* `issuer_id`: Issuer ID of the generated key
* `app_id`: optional, application ID of your app. You can specify `app_id` multiple times with different ids to ingest data for multiple apps.
  * You can also define the app_id in the table name. For example, `app-downloads-detailed:12345,67890` will ingest data for app with id `12345` and `67890`.

## Setting up Appstore Integration


### Prerequisites
To generate an API key, you must have an Admin account in App Store Connect. 

### Generate an API Key

To generate a new API key to use with `ingestr`, log in to [App Store Connect](https://appstoreconnect.apple.com/) and:

1. Select Users and Access, and then select the API Keys tab.
2. Make sure the Team Keys tab is selected.
3. Click Generate API Key or the Add (+) button.
4. Enter a name for the key. The name is for your reference only and isn’t part of the key itself.
5. Under Access, select the role as `FINANCE`.
6. Click Generate.

The new key’s name, key ID, a download link, and other information appears on the page.

For more information, see [App Store Connect docs](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api)

### Find your Apps ID

You can find the App ID of your app by:
1. Opening the app entry in App Store Connect
2. Looking for "General Information" in the App information tab
3. Finding your App ID under the "Apple ID" entry

With this, you are ready to ingest data from App Store.

### Request a Report.
Before you can ingest analytics data from App Store, you need to submit a [Report Request](https://developer.apple.com/documentation/appstoreconnectapi/post-v1-analyticsreportrequests). See [App Store Connect docs](https://developer.apple.com/documentation/appstoreconnectapi/downloading-analytics-reports) for more information on how to Request a Report.

We recommend using `ONGOING` access-type for reports. Please note that it may take upto 48 hours after submitting a Report Request for the data to become available. For more information, see [Request analytics report](https://developer.apple.com/documentation/appstoreconnectapi/downloading-analytics-reports#Request-analytics-reports).

> [!NOTE]
> you have to create a Report Request for each individual App that you want to ingest data for. You can use [list apps](https://developer.apple.com/documentation/appstoreconnectapi/get-v1-apps) API to get the list of all apps in your Apple Account.
### Example: Loading App Downloads Analytics

For this example, we'll assume that:
* `key_id` is `key_0`
* `issuer_id` is `issue_0`
* `key` is stored in the current directory and is named `api.key`
* `app_id` is `12345`

We will run `ingestr` to save this data to a [duckdb](https://duckdb.org/) database called `analytics.db` under the name `public.app_downloads`.

```sh
ingestr ingest \
    --source-uri "appstore://app_id=12345&key_path=api.key&key_id=key_0&issuer_id=issue_0 \
    --source-table "app-downloads-detailed" \
    --dest-uri "duckdb:///analytics.db"  \
    --dest-table "public.app_downloads" \
```

### Example: Loading Data for multiple Apps

We will extend the prior example with another app with ID `67890`. To achieve this, simply add another `app_id` query parameter to the URI.
```sh
ingestr ingest \
    --source-uri "appstore://app_id=12345&app_id=67890&key_path=api.key&key_id=key_0&issuer_id=issue_0 \
    --source-table "app-downloads-detailed" \
    --dest-uri "duckdb:///analytics.db"  \
    --dest-table "public.app_downloads" \
```


### Example: Incremental Loading

`ingestr` supports incremental loading for all App Store tables.

To begin, we will first load all data till `2025-01-01` by specifying the `--interval-end` flag. We'll assume the same credentials from our [first example](#example-loading-app-downloads-analytics)
```sh
ingestr ingest \
    --source-uri "appstore://app_id=12345&key_path=api.key&key_id=key_0&issuer_id=issue_0 \
    --source-table "app-downloads-detailed" \
    --dest-uri "duckdb:///analytics.db"  \
    --dest-table "public.app_downloads" \
    --interval-end "2025-01-01"
```

`ingestr` will load all data available till `2025-01-01`. Now we will run `ingestr` again, but this time, we'll let `ingestr` pickup from where it left off by specifying the `--incremental-strategy` flag.

```sh
ingestr ingest \
    --source-uri "appstore://app_id=12345&key_path=api.key&key_id=key_0&issuer_id=issue_0 \
    --source-table "app-downloads-detailed" \
    --dest-uri "duckdb:///analytics.db"  \
    --dest-table "public.app_downloads" \
    --incremental-strategy "merge"
```

Notice how we didn't specify a date parameter? `ingestr` will automatically use the metadata from last load and continue loading data from that point on.

## Tables

### `app-downloads-detailed`
The App Downloads Report includes download data generated on the App Store. You can use this report to understand your total number of downloads, including first-time downloads, redownloads, updates, and more.

| **Column**   | **Description** |
|--------------|-----------------|
| `date`  | Date on which the event occurred. |
| `app_name`    | The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`    |  Your app’s Apple ID.  |
| `download_type`    | The type of download event that occured. |
| `app_version` | The app version being downloaded. | 
| `device`     | The device on which the app was downloaded.|
| `platform_version` | The OS version of the device on which the download occured.|
| `source_type` | The source from where the user discovered the app.|
| `source_info` | The app referrer or web referrer that led the user to discover the app.|
| `campaign` | The Campaign Token of the campaign created in App Analytics. Column available starting November 19, 2024.|
| `page_type` | The page type from where the app was downloaded. |
| `page_title` | The name of the product page or in-app event page that led the user to download the app.|
| `pre-order` | A flag indicating whether the download came from a pre-order.|
| `territory` | The App Store country or region where the download occured.|
| `counts` | The total number of downloads.|


### `app-store-discovery-and-engagement-detailed`
The App Store Discovery and Engagement report provides details about how users engage with your apps on the App Store itself. This includes data about user engagement with your app’s icons, product pages, in-app event pages, and other install sheets.


| **Column**   | **Description** |
|--------------|-----------------|
| `date`  | Date on which the event occurred. |
| `app_name`    | The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`    |  Your app’s Apple ID.  |
| `event`    | The type of event that occurred.|
| `source_type` | The source from where the user discovered the app. |
| `source_info` | The app referrer or web referrer that led the user to discover the app.|
| `campaign` | The Campaign Token of the campaign created in App Analytics. Column available starting November 19, 2024.|
| `page_type` | The page type from where the app was downloaded. |
| `page_title` | The name of the product page or in-app event page that led the user to download the app.|
| `device` | The device on which the event occurred. |
| `engagement_type` | User action, if any, on the impression or page.|
| `platform_version` | The OS version of the device on which the event occurred.|
| `territory` | The App Store country or region where the download occured.|
| `counts` | The total number of events that occurred. |
| `unique_counts` | The total number of unique users that performed the event.|

### `app-sessions-detailed`
App Session provides insights on how often people open your app, and how long they spend in your app.

| **Column**   | **Description** |
|--------------|-----------------|
| `date`  | Date on which the event occurred. |
| `app_name`    | The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`    |  Your app’s Apple ID.  |
| `download_type`    | The type of download event that occured. |
| `app_version` | The app version being downloaded. | 
| `device`     | The device on which the app was downloaded.|
| `platform_version` | The OS version of the device on which the download occured.|
| `source_type` | The source from where the user discovered the app.|
| `source_info` | The app referrer or web referrer that led the user to discover the app.|
| `campaign` | The Campaign Token of the campaign created in App Analytics. Column available starting November 19, 2024.|
| `page_type` | The page type from where the app was downloaded. |
| `page_title` | The name of the product page or in-app event page that led the user to download the app.|
| `app_download_date` | The date on which the app was downloaded onto the device. This field is only populated if the download occurred in the previous 30 days, otherwise it is null.|
| `territory` | The App Store country or region where the download occured.|
| `sessions` | The number of sessions. Based on users who have agreed to share their data with Apple and developers.|
| `total_session_duration` | The total duration, in seconds, of all sessions being reported.|
| `unique_devices` | The number of unique devices contributing to the total number of sessions being reported.|

### `app-store-installation-and-deletion-detailed`
Use the data in App Store Installation and Deletion report to estimate the number of times people install and delete your App Store apps.


| **Column**   | **Description** |
|--------------|-----------------|
| `date`| Date on which the event occurred.|
| `app_name`| The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`| Date on which the event occurred.|
| `event`| The type of usage event that occurred. |
| `download_type`| The type of download event that occurred.|
| `app_version`| The version of the app being associated with the instalation or deletion.|
| `device`| The device on which the app was installed or deleted.|
| `platform_version`| The OS version of the device on which the app was installed or deleted.|
| `source_type`| Where the user discovered your app.|
| `source_info`| The app referrer or web referrer that led the user to discover your app.|
| `campaign`| The Campaign Token of the campaign created in App Analytics. Column available starting November 19, 2024.|
| `page_type`| The page type which led the user to discover your app.|
| `page_title`| The name of the product page or in-app event page that led the user to discover your app.|
| `app_download_date`| The date on which the app was downloaded onto the device. This field is only populated if the download occurred in the previous 30 days, otherwise it is null.|
| `territory`| The App Store country or region where the installation or deletion occurred.|
| `counts`| The total count of events, based on users who have agreed to share their data with Apple and developers.|
| `unique_devices`| The number of unique devices on which events were generated, based on users who have agreed to share their data with Apple and developers.|

### `app-store-purchases-detailed`
The App Store Purchases Report includes App Store paid app and in-app purchase data. Using the data in this report, you can measure your total revenue generated on the App Store, attribute sales to download sources and page types, and measure how many paying users you have for each individual row. Paying user counts are not summable across rows, because the same user can exist in multiple rows.

| **Column**   | **Description** |
|--------------|-----------------|
| `date`| Date on which the event occurred.|
| `app_name`| The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`| Your app’s Apple ID.|
| `purchase_type`| The type of purchase made by the user on the App Store.|
| `content_name`| The name of the content being purchased. For paid apps, the field will populate the name of app as set in App Store. For in-app purchases, the field will populate the name of the SKU as set in App Store Connect.|
| `content_apple_identifier`| Your content’s Apple ID |
| `payment_method`| The payment type used to charge the customer.|
| `device`| The device on which the purchase occurred.|
| `platform_version`| The OS version of the device on which the app was installed or deleted.|
| `source_type`| Where the user discovered your app.|
| `source_info`| The app referrer or web referrer that led the user to discover your app.|
| `campaign`| The Campaign Token of the campaign created in App Analytics. Column available starting November 19, 2024.|
| `page_type`| The page type which led the user to discover your app.|
| `page_title`| The name of the product page or in-app event page that led the user to discover your app.|
| `app_download_date`| The date on which the app was downloaded onto the device. This field is only populated if the download occurred in the previous 30 days, otherwise it is null.|
| `pre-order`| Indicates whether the purchase originated from someone who pre-ordered the app.|
| `territory`| The App Store country or region in which the purchase occurred.|
| `purchases`| Aggregated count of purchases made. Negative value indicates refunds. If purchases count is 0 and proceeds, and sales are negative, it indicates partial refunds.|
| `proceeds_in_usd`| The estimated proceeds in USD from purchases of your app and in-app purchases. This is the Customer Price minus applicable taxes and Apple’s commission, per Schedule 2 of the Paid Apps Agreement.|
| `sales_in_usd`| The estimated sales in USD from purchases of your app and in-app purchases.|
| `paying_users`| The number of unique users who paid for your app or in-app purchases. This metric is not summable across rows.|

### `app-crashes-expanded`
Use this report to understand crashes for your App Store apps by app version and device type.

| **Column**   | **Description** |
|--------------|-----------------|
| `date`| Date on which the event occurred.|
| `app_name`| The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`| Your app’s Apple ID.|
| `app_version` | The app version being downloaded. | 
| `device`     | The device on which the app was downloaded.|
| `platform_version` | The OS version of the device on which the download occured.|
| `crashes` | The total number of crashes.|
| `unique_devices` | Number of unique devices where app crashed. |


Use these as `--source-table` parameter in the `ingestr ingest` command.

To know more about these reports and their dimensions, see [App Store Analytics docs](https://developer.apple.com/documentation/analytics-reports).