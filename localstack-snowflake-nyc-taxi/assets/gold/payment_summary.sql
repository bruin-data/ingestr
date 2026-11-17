/* @bruin
name: gold.payment_summary
type: sf.sql
description: |
  How do New Yorkers pay for green taxis, and who tips? One row per payment type
  with trips, revenue and tips for January 2024.
  Caveat: the taxi meter only records tips paid by card, so cash trips always show
  $0 tips. That doesn't mean cash riders never tip.
depends:
  - silver.trips
materialization:
  type: table
columns:
  - name: payment_type
    type: varchar
    primary_key: true
    checks:
      - name: not_null
      - name: unique
  - name: trips
    type: integer
    checks:
      - name: positive
  - name: tip_pct_of_fare
    type: float
    description: Tips as a percentage of the fare. Always 0 for cash, because cash tips aren't recorded.
    checks:
      - name: non_negative
custom_checks:
  - name: trips add up to silver.trips
    query: SELECT ABS((SELECT SUM(trips) FROM gold.payment_summary) - (SELECT COUNT(*) FROM silver.trips))
@bruin */

SELECT
    payment_type,
    COUNT(*) AS trips,
    ROUND(100 * COUNT(*) / SUM(COUNT(*)) OVER (), 1) AS share_of_trips_pct,
    ROUND(SUM(total_amount), 2) AS revenue,
    ROUND(AVG(total_amount), 2) AS avg_total,
    ROUND(AVG(tip_amount), 2) AS avg_tip,
    ROUND(100 * SUM(tip_amount) / NULLIF(SUM(fare_amount), 0), 1) AS tip_pct_of_fare,
    COUNT_IF(tip_amount > 0) AS tipped_trips
FROM silver.trips
GROUP BY payment_type
