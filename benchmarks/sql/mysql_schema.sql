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
    ts_tz_val       DATETIME(6),
    json_val        JSON,
    extra_text      TEXT
);
