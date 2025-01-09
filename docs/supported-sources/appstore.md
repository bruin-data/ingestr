# Apple App Store 
The [App Store](https://appstore.com/) is an app marketplace developed and maintained by Apple, for mobile apps on its iOS and iPadOS operating systems. The store allows users to browse and download approved apps developed within Apple's iOS SDK. Apps can be downloaded on the iPhone, iPod Touch, or iPad, and some can be transferred to the Apple Watch smartwatch or 4th-generation or newer Apple TVs as extensions of iPhone apps.

`ingestr` allows you to ingest analytics, sales and performance data using the [Apple App Store Connect API](https://developer.apple.com/documentation/appstoreconnectapi)

## URI Format

The URI format for App Store is as follows:
```
appstore://?key_path=</path/to/key>&key_id=<key_id>&issuer_id=<issuer_id>&app_id=<app_id>
```

URI Parameters:
* `key_path`: path to API private key
* `key_id`: ID of the generated key
* `issuer_id`: Issuer ID of the generated key
* `app_id`: application ID of your app. You can specify `app_id` multiple times with different ids to ingest data for multiple apps.

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

| **Table**    | **Description**                                                                 |
|---------------|---------------------------------------------------------------------------------|
| `date`  | Date on which the event occurred. |
| `app_name`    | The name of the app provided by you during app setup in App Store Connect.|
| `app_apple_identifier`    |  Your app’s Apple ID.  |
| `download_type`    | The type of download event that occured. Possible values: First-time Download, Redownload, Manual update, Auto-update or Restore |
| `app_version` | The app version being downloaded. | 
| `device`     | The device on which the app was downloaded.|
| `platform_version` | The OS version of the device on which the download occured.|
| `source_type` | The source from where the user discovered the app. Possible values: App Store search, App Store browse, App referrer, Web referrer, App Clip, Unavailable or Institutional purchase|
| `source_info` | The app referrer or web referrer that led the user to discover the app.|
| `campaign` | The Campaign Token of the campaign created in App Analytics. Column available starting November 19, 2024.|
| `page_type` | The page type from where the app was downloaded. Possible values: Product page, In-App event, Store sheet or No Page. |
| `page_title` | The name of the product page or in-app event page that led the user to download the app.|
| `pre-order` | A flag indicating whether the download came from a pre-order.|
| `territory` | The App Store country or region where the download occured.|
| `counts` | The total number of downloads.|


Use these as `--source-table` parameter in the `ingestr ingest` command.

