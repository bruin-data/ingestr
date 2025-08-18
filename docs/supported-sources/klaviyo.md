# Klaviyo

[Klaviyo](https://www.klaviyo.com/) is a marketing automation platform that helps businesses build and manage smarter digital relationships with their customers by connecting through personalized email and enhancing customer loyalty.

ingestr supports Klaviyo as a source.

## URI format

The URI format for Klaviyo is as follows:

```plaintext
klaviyo://?api_key=<api-key>
```

URI parameters:

- `api_key`: The API key used for authentication with the Klaviyo API.

The URI is used to connect to the Klaviyo API for extracting data.

```bash
ingestr ingest --source-table 'events' --source-uri 'klaviyo://?api_key=pk_test' --dest-uri duckdb:///klaviyo.duckdb --interval-start 2022-01-01 --dest-table 'dest.events' --extract-parallelism 20
```

This command fetches all the events that are created/updated since 2022-01-01 and writes them to `dest.events` table on DuckDB, using 20 parallel threads to improve performance and efficiently handle large data .

## Tables

Klaviyo source allows ingesting the following sources into separate tables:

## Tables

Klaviyo source allows ingesting the following sources into separate tables:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [events](https://developers.klaviyo.com/en/reference/get_events) | id | datetime     | merge               | Retrieves all events in an account where each event represents an action taken by a profile such as a password reset or a product order. |
| [profiles](https://developers.klaviyo.com/en/reference/get_profiles) | id | updated    | merge             |  Retrieves all profiles in an account where each profile includes details like organization, job title, email and other attributes.|
| [campaigns](https://developers.klaviyo.com/en/reference/get_campaigns) | id | updated_at     | merge               | Retrieves all campaigns in an account where each campaign is a targeted message sent to a specific audience. |
| [metrics](https://developers.klaviyo.com/en/reference/get_metrics) | id | updated    | merge               | Retrieves all metrics in an account where each metric represents a category of events or actions a person can take. |
| [tags](https://developers.klaviyo.com/en/reference/get_tags) | id | –                | replace               | Retrieves all tags in an account. |
| [coupons](https://developers.klaviyo.com/en/reference/get_coupons) | id | –                | replace               | Retrieves all coupons in an account.|
| [catalog-variants](https://developers.klaviyo.com/en/reference/get_catalog_variants): | id | updated    | merge               | Retrieves all variants in an account. |
| [catalog-categories](https://developers.klaviyo.com/en/reference/get_catalog_categories) | id | updated   | merge               | Retrieves all catalog categories in an account.|
| [catalog-items](https://developers.klaviyo.com/en/reference/get_catalog_items)| id | updated    | merge               | Retrieves all catalog items in an account. |
| [flows](https://developers.klaviyo.com/en/reference/get_flows) | id | updated     | merge               | Retrieves all flows in an account where flow is a sequence of automated actions that is triggered when a person performs a specific action. |
| [lists](https://developers.klaviyo.com/en/reference/get_lists) | id | updated     | merge               |  Retrieves all lists in an account.|
| [images](https://developers.klaviyo.com/en/reference/get_images) | id | updated_at     | merge               | Retrieves all images in an account. |
| [segments](https://developers.klaviyo.com/en/reference/get_segments) | id | updated    | merge               | Retrieves all segments in an account where segment is a dynamic list that contains profiles meeting a certain set of conditions. |
| [forms](https://developers.klaviyo.com/en/reference/get_forms) | id | updated_at    | merge               | Retrieves all forms in an account.|
| [templates](https://developers.klaviyo.com/en/reference/get_templates) | id | updated     | merge               |  Retrieves all templates in an account. |

Use these as `--source-table` parameter in the `ingestr ingest` command.


> [!WARNING]
> Klaviyo does not support incremental loading for many endpoints in its APIs, which means ingestr will load endpoints incrementally if they support it, and do a full-refresh if not.
