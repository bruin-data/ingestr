# Facebook Ads

Facebook Ads is the advertising platform that helps users to create targeted ads on Facebook, Instagram and Messenger.

ingestr supports Facebook Ads as a source using [Facebook Marketing API](https://developers.facebook.com/docs/marketing-api/).

## URI format

The URI format for Facebook Ads is as follows:

```plaintext
facebookads://?access_token=<access_token>&account_id=<account_id>
```

URI parameters:

- `access_token` (required) is associated with Business Facebook App.
- `account_id` (optional) is associated with Ad manager. Can also be provided in the table name.

The URI is used to connect to Facebook Ads API for extracting data.

## Setting up a Facebook Ads Integration

Facebook Ads requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/facebook_ads#setup-guide).

Once you complete the guide, you should have an access token and an Account ID. Let's say your `access_token` is `abcdef` and `account_id` is `1234`, here's a sample command that will copy the data from Facebook Ads into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.campaigns'
```

The result of this command will be a table in the `facebook.duckdb` database.

## Tables

Facebook Ads source allows ingesting the following sources into separate tables:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `campaigns`       | id | –  |        replace     | Retrieves campaign data with `fields`: id, updated_time, created_time, name, status, effective_status, objective, start_time, stop_time, daily_budget, lifetime_budget              |
| `campaigns:account_id1,account_id2`       | id | –  |        replace     | Same as above but with account IDs specified in table name (supports multiple accounts)              |
| `ad_sets` | id | –                | replace            | Retrieves ad set data with `fields`: id, updated_time, created_time, name, status, effective_status, campaign_id, start_time, end_time, daily_budget, lifetime_budget, optimization_goal, promoted_object, billing_event, bid_amount, bid_strategy, targeting                       |
| `ad_sets:account_id1,account_id2` | id | –                | replace            | Same as above but with account IDs specified in table name (supports multiple accounts)                       |
| `ads`   | id | -     | replace  | Retrieves ad data with `fields`: id, updated_time, created_time, name, status, effective_status, adset_id, campaign_id, creative, targeting, tracking_specs, conversion_specs                          |
| `ads:account_id1,account_id2`   | id | -     | replace  | Same as above but with account IDs specified in table name (supports multiple accounts)                          |
| `ad_creatives`   | id | -     | replace  | Retrieves ad creative data with `fields`: id, name, status, thumbnail_url, object_story_spec, effective_object_story_id, call_to_action_type, object_type, template_url, url_tags, instagram_actor_id, product_set_id |
| `ad_creatives:account_id1,account_id2`   | id | -     | replace  | Same as above but with account IDs specified in table name (supports multiple accounts) |
| `leads`   | id | -     | replace  | Retrieves lead data with fields: id, created_time, ad_id, ad_name, adset_id, adset_name, campaign_id, campaign_name, form_id, field_data |
| `leads:account_id1,account_id2`   | id | -     | replace  | Same as above but with account IDs specified in table name (supports multiple accounts) |
| `facebook_insights`   | date_start | date_start     | merge  | Retrieves insights data (requires account_id in URI) |
| `facebook_insights_with_account_ids:account_id1,account_id2`   | date_start | date_start     | merge  | Retrieves insights data for multiple accounts |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Account ID in Table Name

For `campaigns`, `ad_sets`, `ads`, `ad_creatives`, and `leads`, you can specify account ID(s) directly in the table name instead of the URI:

```sh
# Single account in table name
ingestr ingest \
  --source-uri 'facebookads://?access_token=your_token' \
  --source-table 'campaigns:1234567890' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.campaigns'

# Multiple accounts in table name
ingestr ingest \
  --source-uri 'facebookads://?access_token=your_token' \
  --source-table 'campaigns:1234567890,9876543210' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.campaigns'
```

### Facebook Insights Custom Configuration

The `facebook_insights` table supports advanced configuration for breakdowns and custom fields:

#### Format Options

There are two distinct configuration modes:

**Mode 1: Predefined Breakdowns**
- **Format**: `facebook_insights:breakdown_type` or `facebook_insights:breakdown_type:field1,field2,field3`
- Uses a predefined breakdown type (see list below)
- Cannot be combined with custom dimensions
- Custom fields are optional - if omitted, uses default fields for that breakdown

**Mode 2: Custom Dimensions**
- **Format**: `facebook_insights:dimension1,dimension2:field1,field2,field3` or `facebook_insights:level,dimension1,dimension2:field1,field2,field3`
- Uses custom dimensions (not predefined breakdown names)
- **Fields are required** - you must always provide fields after the second colon
- Can optionally specify a level (account, campaign, adset, ad) as the first dimension

> [!TIP]
> You can include `campaign_id`, `adset_id`, and `ad_id` in your fields list alongside your metrics if you want to have these as part of the output. 
> 
> E.g. `facebook_insights:ad:campaign_id,adset_id,ad_id,spend,impressions,clicks`

#### Available Predefined Breakdown Types

- `ads_insights` - Basic insights without breakdowns
- `ads_insights_age_and_gender` - Breakdown by age and gender
- `ads_insights_country` - Breakdown by country
- `ads_insights_platform_and_device` - Breakdown by platform and device
- `ads_insights_region` - Breakdown by region
- `ads_insights_dma` - Breakdown by DMA (Designated Market Area)
- `ads_insights_hourly_advertiser` - Breakdown by hour (advertiser time zone)

#### Available Levels

When using **custom dimensions** (not predefined breakdowns), you can specify one of these levels as the first dimension:
- `account` - Account level insights
- `campaign` - Campaign level insights  
- `adset` - Ad set level insights
- `ad` - Ad level insights

Note: 
- Levels can only be used with custom dimensions, not with predefined breakdowns
- If multiple levels are specified in the dimensions list, the last valid level will be used and removed from the dimensions list

#### Common Dimensions

When using **custom dimensions** (not predefined breakdowns), you can use any valid Facebook Ads dimension. Some commonly used dimensions include:
- `age` - Age ranges
- `gender` - Gender breakdown
- `country` - Country breakdown
- `region` - Region breakdown  
- `platform_position` - Platform position
- `publisher_platform` - Publisher platform
- `impression_device` - Device type
- `placement` - Ad placement

**Important Notes:**
- Custom dimensions **must always be accompanied by fields** (after the second colon)
- Predefined breakdown names (like `ads_insights_age_and_gender`) cannot be used as custom dimensions
- Not all dimension combinations are valid according to Facebook's API. Refer to [Facebook's Marketing API](https://developers.facebook.com/docs/marketing-api/insights/breakdowns/) documentation for valid dimension combinations

#### Examples

```sh
# Predefined breakdown: Basic insights without breakdowns
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ads_insights' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_basic'

# Predefined breakdown: Age and gender with default fields
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ads_insights_age_and_gender' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_demographics'

# Predefined breakdown: Country with custom fields
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ads_insights_country:campaign_id,adset_id,ad_id,impressions,clicks,spend,reach,cpm,ctr' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_by_country'

# Predefined breakdown: Platform and device with default fields
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ads_insights_platform_and_device' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_platform_device'

# Custom dimensions: Age and gender with custom fields (fields required)
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:age,gender:campaign_id,adset_id,ad_id,impressions,clicks,spend' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_custom_dimensions'

# Campaign level with custom dimensions and fields
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:campaign,age,gender:campaign_id,adset_id,ad_id,impressions,clicks,spend,reach' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.campaign_insights_demographics'

# Ad level with geographic dimensions
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ad,country,age:campaign_id,adset_id,ad_id,clicks,impressions,spend' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.ad_insights_geographic'

# Account level insights only (no additional dimensions, fields required)
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:account:campaign_id,adset_id,ad_id,impressions,clicks,spend,reach' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.account_level_insights'

# Adset level with single dimension
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:adset,gender:campaign_id,adset_id,ad_id,spend' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.adset_gender_insights'
```

### Facebook Insights with Multiple Accounts

Use `facebook_insights_with_account_ids` to fetch insights from multiple accounts in a single request. The account IDs are specified in the table name.

#### Format

```plaintext
facebook_insights_with_account_ids:account_id1,account_id2
facebook_insights_with_account_ids:account_id1,account_id2:breakdown_type
facebook_insights_with_account_ids:account_id1,account_id2:breakdown_type:field1,field2
```

#### Examples

```sh
# Basic insights from multiple accounts
ingestr ingest \
  --source-uri 'facebookads://?access_token=your_token' \
  --source-table 'facebook_insights_with_account_ids:1234567890,9876543210' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.multi_account_insights' \
  --interval-start 2024-12-01 \
  --interval-end 2024-12-31

# Multiple accounts with predefined breakdown
ingestr ingest \
  --source-uri 'facebookads://?access_token=your_token' \
  --source-table 'facebook_insights_with_account_ids:1234567890,9876543210:ads_insights_age_and_gender' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.multi_account_demographics' \
  --interval-start 2024-12-01 \
  --interval-end 2024-12-31

# Multiple accounts with breakdown and custom fields
ingestr ingest \
  --source-uri 'facebookads://?access_token=your_token' \
  --source-table 'facebook_insights_with_account_ids:1234567890,9876543210:ads_insights_country:impressions,clicks,spend' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.multi_account_by_country' \
  --interval-start 2024-12-01 \
  --interval-end 2024-12-31
```

> [!NOTE]
> When using `facebook_insights_with_account_ids`, the `account_id` parameter in the URI is ignored. Account IDs must be provided in the table name.
