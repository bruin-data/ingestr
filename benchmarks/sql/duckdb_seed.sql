DROP TABLE IF EXISTS BENCH_TABLE_PLACEHOLDER;

CREATE TABLE BENCH_TABLE_PLACEHOLDER AS
SELECT
    g::INTEGER                                                          AS id,
    'name_' || (g % 10000)                                              AS small_str,
    'user_' || g || '@example-' || (g % 500) || '.com'                  AS medium_str,
    repeat(chr(65 + (g % 26)::INTEGER), (50 + (g % 200))::INTEGER)     AS large_str,
    (g % 32767)::SMALLINT                                               AS tiny_int,
    g::INTEGER                                                          AS regular_int,
    (g * 1000000)::BIGINT                                               AS big_int,
    (g::DOUBLE / 7.0) + (g % 1000)::DOUBLE                             AS float_val,
    ((g % 1000000)::DECIMAL(18,4)) / 100.0                             AS decimal_val,
    (g % 2 = 0)::BOOLEAN                                                AS bool_val,
    ('2020-01-01'::DATE + INTERVAL (g % 1500) DAY)::DATE               AS date_val,
    ('2020-01-01'::TIMESTAMP + INTERVAL (g) SECOND)::TIMESTAMP          AS ts_val,
    ('2020-01-01 00:00:00+00'::TIMESTAMPTZ + INTERVAL (g) SECOND)::TIMESTAMPTZ AS ts_tz_val,
    '{"key": "val_' || (g % 100) || '", "num": ' || g || '}'           AS json_val,
    'extra_text_row_' || g || '_' || repeat('x', (50 + (g % 100))::INTEGER) AS extra_text
FROM range(1, BENCH_ROWS_PLACEHOLDER + 1) AS t(g);
