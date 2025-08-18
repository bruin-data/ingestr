# Facebook Ads

Facebook Ads is the advertising platform that helps users to create targeted ads on Facebook, Instagram and Messenger.

ingestr supports Facebook Ads as a source using [Facebook Marketing API](https://developers.facebook.com/docs/marketing-api/).

## URI format

The URI format for Facebook Ads is as follows:

```plaintext
facebookads://?access_token=<access_token>&account_id=<account_id>
```

URI parameters:

- `access_token` is associated with Business Facebook App.
- `account_id` is associated with Ad manager.

Both are used for authentication with Facebook Ads API.

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
| `ad_sets` | id | –                | replace            | Retrieves ad set data with `fields`: id, updated_time, created_time, name, status, effective_status, campaign_id, start_time, end_time, daily_budget, lifetime_budget, optimization_goal, promoted_object, billing_event, bid_amount, bid_strategy, targeting                       |
| `ads`   | id | -     | replace  | Retrieves ad data with `fields`: id, updated_time, created_time, name, status, effective_status, adset_id, campaign_id, creative, targeting, tracking_specs, conversion_specs                          |
| `ad_creatives`   | id | -     | replace  | Retrieves ad creative data with `fields`: id, name, status, thumbnail_url, object_story_spec, effective_object_story_id, call_to_action_type, object_type, template_url, url_tags, instagram_actor_id, product_set_id |
| `leads`   | id | -     | replace  | Retrieves lead data with fields: id, created_time, ad_id, ad_name, adset_id, adset_name, campaign_id, campaign_name, form_id, field_data |
| `facebook_insights`   | date_start | date_start     | merge  | Retrieves insights data with `fields`: campaign_id, adset_id, ad_id, date_start, date_stop, reach, impressions, frequency, clicks, unique_clicks, ctr, unique_ctr, cpc, cpm, cpp, spend, actions, action_values, cost_per_action_type, website_ctr, account_currency, ad_click_actions, ad_name, adset_name, campaign_name, country, dma, full_view_impressions, full_view_reach, inline_link_click_ctr, outbound_clicks, social_spend, conversions, video_thruplay_watched_actions. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Facebook Insights Custom Configuration

The `facebook_insights` table supports advanced configuration for breakdowns and custom metrics:

#### Format Options

1. **Default usage**: `facebook_insights`
   - Uses default breakdown and default fields

2. **Predefined breakdown**: `facebook_insights:breakdown_type`
   - Uses specified predefined breakdown with default fields

3. **Predefined breakdown + custom metrics**: `facebook_insights:breakdown_type:metric1,metric2,metric3`
   - Uses specified predefined breakdown with custom metrics

4. **Custom dimensions + metrics**: `facebook_insights:dimension1,dimension2:metric1,metric2,metric3`
   - Uses custom dimensions with custom metrics

5. **Level + dimensions + metrics**: `facebook_insights:level,dimension1,dimension2:metric1,metric2,metric3`
   - Uses specified level with custom dimensions and metrics

#### Available Predefined Breakdown Types

- `ads_insights` (default)
- `ads_insights_age_and_gender`
- `ads_insights_country`
- `ads_insights_platform_and_device`
- `ads_insights_region`
- `ads_insights_dma`
- `ads_insights_hourly_advertiser`

#### Available Levels

When using custom dimensions, you can specify one of these levels:
- `account` - Account level insights
- `campaign` - Campaign level insights  
- `adset` - Ad set level insights
- `ad` - Ad level insights

Note: If multiple levels are specified in the dimensions list, the last valid level will be used and removed from the dimensions list.

#### Common Dimensions

You can use any valid Facebook Ads dimension in your custom configurations. Some commonly used dimensions include:
- `age` - Age ranges
- `gender` - Gender breakdown
- `country` - Country breakdown
- `region` - Region breakdown  
- `platform_position` - Platform position
- `publisher_platform` - Publisher platform
- `impression_device` - Device type
- `placement` - Ad placement

Note: Not all dimension combinations are valid according to Facebook's API. Refer to [Facebook's Marketing API](https://developers.facebook.com/docs/marketing-api/insights/breakdowns/) documentation for valid dimension combinations.

#### Examples

```sh
# Default facebook_insights
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights'

# Age and gender breakdown with default metrics
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ads_insights_age_and_gender' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_demographics'

# Country breakdown with custom metrics
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ads_insights_country:impressions,clicks,spend,reach,cpm,ctr' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_by_country'

# Custom dimensions (age and gender) with custom metrics
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:age,gender:impressions,clicks,spend' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.insights_custom_dimensions'

# Campaign level with custom dimensions and metrics
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:campaign,age,gender:impressions,clicks,spend,reach' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.campaign_insights_demographics'

# Ad level with geographic dimensions
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:ad,country,region:clicks,impressions,spend' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.ad_insights_geographic'

# Account level insights only (no additional dimensions)
ingestr ingest \
  --source-uri 'facebookads://?access_token=easdyh&account_id=1234' \
  --source-table 'facebook_insights:account:impressions,clicks,spend,reach' \
  --dest-uri 'duckdb:///facebook.duckdb' \
  --dest-table 'dest.account_level_insights'
```
