SET SESSION cte_max_recursion_depth = 100000001;

DROP TABLE IF EXISTS BENCH_TABLE_PLACEHOLDER;

CREATE TABLE BENCH_TABLE_PLACEHOLDER (
    id              INT PRIMARY KEY,
    small_str       VARCHAR(20),
    medium_str      VARCHAR(100),
    large_str       VARCHAR(500),
    tiny_int        SMALLINT,
    regular_int     INT,
    big_int         BIGINT,
    float_val       DOUBLE,
    decimal_val     DECIMAL(18,4),
    bool_val        BOOLEAN,
    date_val        DATE,
    ts_val          DATETIME(6),
    ts_tz_val       TIMESTAMP(6),
    json_val        JSON,
    extra_text      TEXT
);

INSERT INTO BENCH_TABLE_PLACEHOLDER
WITH RECURSIVE seq (g) AS (
    SELECT 1 UNION ALL SELECT g + 1 FROM seq WHERE g < BENCH_ROWS_PLACEHOLDER
)
SELECT
    g,
    CONCAT('name_', g % 10000),
    CONCAT('user_', g, '@example-', g % 500, '.com'),
    REPEAT(CHAR(65 + (g % 26)), 50 + (g % 200)),
    g % 32767,
    g,
    g * 1000000,
    (g / 7.0) + (g % 1000),
    (g % 1000000) / 100.0,
    (g % 2 = 0),
    DATE_ADD('2020-01-01', INTERVAL (g % 1500) DAY),
    DATE_ADD('2020-01-01 00:00:00', INTERVAL g SECOND),
    DATE_ADD('2020-01-01 00:00:00', INTERVAL g SECOND),
    JSON_OBJECT('key', CONCAT('val_', g % 100), 'num', g),
    CONCAT('extra_text_row_', g, '_', REPEAT('x', 50 + (g % 100)))
FROM seq;
