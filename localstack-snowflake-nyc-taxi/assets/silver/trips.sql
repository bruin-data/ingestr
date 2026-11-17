/* @bruin
name: silver.trips
type: sf.sql
description: Cleaned January 2024 trips with pickup zone, borough and payment type. Every trip chart reads this table.
depends:
  - bronze.taxi_trips
  - bronze.taxi_zones
materialization:
  type: table
columns:
  - name: pickup_at
    type: timestamp
    checks:
      - name: not_null
  - name: trip_minutes
    type: double
    checks:
      - name: non_negative
  - name: total_amount
    type: double
    checks:
      - name: non_negative
  - name: payment_type
    type: varchar
    checks:
      - name: accepted_values
        value: ["flex_fare", "credit_card", "cash", "no_charge", "dispute", "unknown", "voided_trip"]
custom_checks:
  - name: no trip is longer than 6 hours
    query: SELECT COUNT(*) FROM silver.trips WHERE trip_minutes > 360
@bruin */

SELECT
    t.lpep_pickup_datetime AS pickup_at,
    t.lpep_dropoff_datetime AS dropoff_at,
    DATEDIFF('second', t.lpep_pickup_datetime, t.lpep_dropoff_datetime) / 60.0 AS trip_minutes,
    z.zone AS pickup_zone,
    z.borough AS pickup_borough,
    t.passenger_count,
    t.trip_distance,
    DECODE(t.payment_type, 0, 'flex_fare', 1, 'credit_card', 2, 'cash', 3, 'no_charge',
           4, 'dispute', 6, 'voided_trip', 'unknown') AS payment_type,
    t.fare_amount,
    t.tip_amount,
    t.total_amount
FROM bronze.taxi_trips t
LEFT JOIN bronze.taxi_zones z ON z.location_id::INT = t.pu_location_id
WHERE t.lpep_pickup_datetime >= '2024-01-01'
  AND t.lpep_pickup_datetime < '2024-02-01'
  AND t.lpep_dropoff_datetime >= t.lpep_pickup_datetime
  -- Seconds, like trip_minutes: DATEDIFF('minute') counts minute boundaries and would let 6h00m30s through.
  AND DATEDIFF('second', t.lpep_pickup_datetime, t.lpep_dropoff_datetime) <= 360 * 60
  AND t.total_amount >= 0
