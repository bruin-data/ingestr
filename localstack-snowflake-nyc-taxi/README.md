# localstack-snowflake-nyc-taxi: Bruin demo on LocalStack Snowflake

Loads public NYC data for January 2024 (green taxi trips, taxi zones and Central Park
weather) straight from their URLs into the LocalStack
Snowflake emulator with ingestr, then builds dashboard tables with SQL.

The assets follow the medallion layout: each layer has its own folder under `assets/` and its own
Snowflake schema.

| Layer | Asset | Type | What it is |
|---|---|---|---|
| Bronze | `bronze.taxi_trips` | ingestr | TLC trip file (~56k trips), loaded as-is |
| Bronze | `bronze.taxi_zones` | ingestr | TLC zone lookup (265 zones) |
| Bronze | `bronze.weather` | ingestr | Open-Meteo daily weather (one JSON document) |
| Silver | `silver.trips` | sf.sql table | cleaned trips with zone, borough and payment type |
| Gold | `gold.daily_trips` | sf.sql table | one row per day: trips, fare, weather |
| Gold | `gold.trips_by_weather` | sf.sql view | do people take more taxis when it rains or snows? |
| Gold | `gold.payment_summary` | sf.sql table | trips, revenue and tips per payment type (cash tips aren't recorded, so they show $0) |

## Create it

```bash
bruin init localstack-snowflake-nyc-taxi
```

## Start the emulator

```bash
export LOCALSTACK_AUTH_TOKEN=<your-token>
docker pull localstack/snowflake-next:latest
docker run -d --name localstack-snowflake -p 127.0.0.1:4567:4566 \
  -e LOCALSTACK_AUTH_TOKEN localstack/snowflake-next:latest
```

See LocalStack's [Snowflake installation guide](https://docs.localstack.cloud/snowflake/getting-started/installation/)
and [auth token page](https://docs.localstack.cloud/snowflake/getting-started/auth-token/) for details.

## `.bruin.yml` setup

`bruin init` adds these connections to your project's `.bruin.yml`. The pipeline and dashboards use
them; to set them up by hand, add them under your `default` environment:

```yaml
environments:
  default:
    connections:
      snowflake:
        - name: snowflake-localstack-demo
          account: test
          username: test
          password: test
          host: snowflake.localhost.localstack.cloud
          port: 4567
          database: LOCALSTACK_DEMO
          warehouse: COMPUTE_WH
      http:
        - name: nyc-taxi-trips-2024-01
          url: https://d37ci6vzurychx.cloudfront.net/trip-data/green_tripdata_2024-01.parquet
        - name: nyc-weather-2024-01
          url: https://archive-api.open-meteo.com/v1/archive?latitude=40.78&longitude=-73.97&start_date=2024-01-01&end_date=2024-01-31&daily=temperature_2m_max,precipitation_sum,snowfall_sum&timezone=America%2FNew_York
        - name: nyc-taxi-zones
          url: https://d37ci6vzurychx.cloudfront.net/misc/taxi_zone_lookup.csv
```

`test`/`test` is the emulator's built-in login. `port` is the host port from `-p` in the `docker run`
above (4567). If you map a different port, change it here and in the `snow sql` command below.

## Run

Run these from the generated `localstack-snowflake-nyc-taxi/` folder. `setup.sql` needs the [Snowflake CLI](https://docs.snowflake.com/en/developer-guide/snowflake-cli/installation/installation) (`snow`):

```bash
# Once: create the warehouse, database and the BRONZE, SILVER and GOLD schemas
snow sql --temporary-connection --account test --user test \
  --password test --host snowflake.localhost.localstack.cloud --port 4567 -f setup.sql

# Run
bruin run .

# See what feeds a table
bruin lineage --full assets/gold/trips_by_weather.sql
```

## Dashboards

`dashboards/` holds two [DAC](https://getbruin.com/docs/dac) dashboards (Bruin's dashboards-as-code tool)
that read the pipeline's tables through the same `snowflake-localstack-demo` connection.

**NYC Green Taxis, January 2024** (`nyc-taxi.yml`) shows the data in four sections:

| Section | Widgets |
|---|---|
| At a glance | KPI tiles, daily trips and revenue (combo), running revenue (area), unusual days (XmR control chart) |
| Weather | trips by weather (bar), fare difference vs dry days (forest plot), temperature vs trips (scatter) |
| Payments and tips | payment mix (donut), trip-to-tip funnel, card revenue (gauge), tip sizes (histogram), trip totals (boxplot) |
| Where green taxis pick up | trips by borough (bar), weekday × borough (heatmap), tabs with a zone table with sparklines and a pivot table |

Filter by weather and borough at the top, or click a weather bar or a borough bar to filter the page. Every
widget follows both filters, except the bar you click with (so you can pick another value) and the forest
plot's weather comparison (it always compares with dry days).

**NYC Taxi Pipeline Health** (`pipeline-health.yml`) shows rows per layer (bronze, silver, gold), quality checks
passing, the last run time, and tabs listing each check and table. The checks are the pipeline's 20 Bruin
checks re-run as SQL on the current tables, because Bruin's own results aren't stored in Snowflake.

Run the pipeline first, then from the same folder:

```bash
# Install DAC once: curl -LsSf https://getbruin.com/install/dac | sh
dac serve --dir . --open   # http://localhost:8321
dac check --dir .          # run every widget's query
```

Each widget runs its own query, so each page takes a few seconds to fill in on the emulator.
