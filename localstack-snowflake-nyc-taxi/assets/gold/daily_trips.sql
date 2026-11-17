/* @bruin
name: gold.daily_trips
type: sf.sql
description: One row per day of January 2024 with taxi trips, average fare and the weather.
depends:
  - silver.trips
  - bronze.weather
materialization:
  type: table
columns:
  - name: day
    type: date
    primary_key: true
    checks:
      - name: not_null
      - name: unique
  - name: trips
    type: integer
    checks:
      - name: positive
  - name: weather
    type: varchar
    checks:
      - name: not_null
      - name: accepted_values
        value: ["dry", "rain", "snow"]
custom_checks:
  - name: has all 31 days of January
    query: SELECT ABS(COUNT(*) - 31) FROM gold.daily_trips
@bruin */

-- Open-Meteo returns one document with parallel arrays; FLATTEN turns it into one row per day.
WITH weather AS (
    SELECT
        d.value::DATE AS day,
        w.daily:temperature_2m_max[d.index]::FLOAT AS temp_max_c,
        w.daily:precipitation_sum[d.index]::FLOAT AS rain_mm,
        w.daily:snowfall_sum[d.index]::FLOAT AS snow_cm
    FROM bronze.weather w,
        LATERAL FLATTEN(input => w.daily:time) d
),
trips AS (
    SELECT pickup_at::DATE AS day, COUNT(*) AS trips, AVG(total_amount) AS avg_fare
    FROM silver.trips
    GROUP BY 1
)
SELECT
    t.day,
    t.trips,
    ROUND(t.avg_fare, 2) AS avg_fare,
    w.temp_max_c,
    w.rain_mm,
    w.snow_cm,
    CASE
        WHEN w.snow_cm > 0 THEN 'snow'
        WHEN w.rain_mm >= 5 THEN 'rain'
        WHEN w.rain_mm IS NOT NULL THEN 'dry'
    END AS weather
FROM trips t
LEFT JOIN weather w ON w.day = t.day
