/* @bruin
name: gold.trips_by_weather
type: sf.sql
description: Do New Yorkers take more taxis when it rains or snows? Average daily trips per kind of day.
depends:
  - gold.daily_trips
materialization:
  type: view
columns:
  - name: weather
    type: varchar
    checks:
      - name: unique
@bruin */

SELECT
    weather,
    COUNT(*) AS days,
    ROUND(AVG(trips)) AS avg_daily_trips,
    ROUND(AVG(avg_fare), 2) AS avg_fare
FROM gold.daily_trips
GROUP BY weather
