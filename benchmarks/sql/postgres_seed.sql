DROP TABLE IF EXISTS public.BENCH_TABLE_PLACEHOLDER;

CREATE TABLE public.BENCH_TABLE_PLACEHOLDER (
    id              INTEGER PRIMARY KEY,
    small_str       VARCHAR(20),
    medium_str      VARCHAR(100),
    large_str       VARCHAR(500),
    tiny_int        SMALLINT,
    regular_int     INTEGER,
    big_int         BIGINT,
    float_val       DOUBLE PRECISION,
    decimal_val     NUMERIC(18,4),
    bool_val        BOOLEAN,
    date_val        DATE,
    ts_val          TIMESTAMP,
    ts_tz_val       TIMESTAMPTZ,
    json_val        JSONB,
    extra_text      TEXT
);
