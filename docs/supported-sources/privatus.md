# Privatus Analytics

[Privatus Analytics](https://privatusanalytics.com/) is website analytics that counts visitors without cookies, so sites can drop the cookie pop-up and still see where visitors come from. It also filters out bot traffic.

ingestr supports Privatus Analytics as a source. Use it to put your website traffic next to your sales, sign-ups and ad costs in your own warehouse.

## URI format

```plaintext
privatus://?api_key=<api_token>
```

URI parameters:

- `api_key` (required): an API token. It starts with `pat_` and reads one workspace.
- `site_ids` (optional): a comma-separated list of site IDs to copy, for example `pa_AB12CD34EF,pa_XY98ZW76VU`. Leave it out to copy every site in the workspace.
- `requests_per_hour` (optional): the API request limit of your plan. The default is `1000`, the Free plan limit. Set it to `5000` on Pro or `20000` on Business so large copies finish sooner.

## Setting up a Privatus Analytics integration

1. Sign in at [privatusanalytics.com](https://privatusanalytics.com/).
2. Open **Workspace settings**, then **API tokens**.
3. Make a new token and copy it.

The free plan includes API access, so you can try this without paying.

Copy the last 30 days of daily stats into DuckDB:

```sh
ingestr ingest \
  --source-uri "privatus://?api_key=pat_xxx" \
  --source-table "daily_stats" \
  --dest-uri duckdb:///privatus.duckdb \
  --dest-table "privatus.daily_stats"
```

Copy top pages for a set date range from one site:

```sh
ingestr ingest \
  --source-uri "privatus://?api_key=pat_xxx&site_ids=pa_AB12CD34EF" \
  --source-table "pages" \
  --interval-start 2026-01-01 \
  --interval-end 2026-01-31 \
  --dest-uri duckdb:///privatus.duckdb \
  --dest-table "privatus.pages"
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
|---|---|---|---|---|
| `workspaces` | `id` | - | replace | The workspace the token belongs to: name, time zone, currency and plan. |
| `sites` | `id` | - | replace | Each site with its domain, time zone and privacy settings. |
| `goals` | `id` | - | replace | Goals set up on each site, active and archived. |
| `daily_stats` | `site_id`, `date` | `date` | merge | One row per site per day: visitors, visits, pageviews, views per visit, bounce rate, visit duration and revenue. |
| `pages` | `site_id`, `date`, `value` | `date` | merge | Pageviews, visits, time on page and scroll depth per page path per day. |
| `referrers` | `site_id`, `date`, `value` | `date` | merge | Visitors per referring domain per day. An empty `value` means a direct visit. |
| `events` | `site_id`, `date`, `value` | `date` | merge | Completions, visits and revenue per custom event per day. |
| `goal_conversions` | `site_id`, `date`, `goal_id` | `date` | merge | Completions, conversions, conversion rate and revenue per goal per day. |
| `breakdown:<dimension>` | `site_id`, `date`, `value` | `date` | merge | Any other dimension per day, for example `breakdown:country`, `breakdown:channel`, `breakdown:utm_campaign` or `breakdown:prop:plan`. |

Every row from a site has `site_id`, so all tables join on it. Days follow the time zone set on each site. Durations are in milliseconds, rates are percents from 0 to 100, and revenue is in cents.

Without `--interval-start` and `--interval-end`, the daily tables copy the last 30 days. Each run replaces the rows for the days it reads, so running it daily keeps today's numbers right as the day goes on.

The per-page, per-referrer and other breakdown tables need one request per site per day. A long date range on many sites can take a while on the Free plan. See the [API docs](https://privatusanalytics.com/docs/api) for the full list of dimensions.
