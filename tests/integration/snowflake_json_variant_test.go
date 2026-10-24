//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// COPY INTO from parquet stores a string in a VARIANT column verbatim, so JSON
// columns used to land as TYPEOF = VARCHAR and path lookups returned NULL. The
// JSON-aware COPY must parse them while leaving SQL NULL as SQL NULL and
// loading every other type exactly as the MATCH_BY_COLUMN_NAME COPY did.
func TestSnowflakeReplaceLoadsPostgresJSONBAsParsedVariant(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	pg, err := sql.Open("pgx", sourceURI)
	require.NoError(t, err)
	defer func() { _ = pg.Close() }()

	srcTable := sourceSchema + ".events"
	_, err = pg.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id INTEGER PRIMARY KEY,
		data JSONB,
		tags JSONB,
		amount NUMERIC(10,2),
		active BOOLEAN,
		happened_at TIMESTAMPTZ,
		recorded_at TIMESTAMP,
		day DATE,
		raw BYTEA,
		note TEXT,
		labels TEXT[]
	)`, srcTable))
	require.NoError(t, err)
	_, err = pg.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
		(1, '{"a":1,"b":{"c":[1,2]}}', '["x","y"]', 12.34, true, '2026-01-02T03:04:05Z', '2026-01-02 03:04:05', '2026-01-02', '\xDEADBEEF', 'one', ARRAY['p','q']),
		(2, '{"s":"it''s"}', '[]', NULL, false, NULL, NULL, NULL, NULL, NULL, ARRAY[]::TEXT[]),
		(3, NULL, NULL, 0, NULL, NULL, NULL, NULL, NULL, 'sql-null', NULL),
		(4, 'null', '"str"', NULL, NULL, NULL, NULL, NULL, NULL, 'json-null', NULL)`, srcTable))
	require.NoError(t, err)

	destTable := "PUBLIC.JSON_REPLACE_" + uniqueSuffix()
	sf, err := snowflakeOpenDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+destTable)
		_ = sf.Close()
	})

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         srcTable,
		DestURI:             snowflakeTestURI,
		DestTable:           destTable,
		IncrementalStrategy: "replace",
		StagingDataset:      "PUBLIC",
		NoLoadTimestamp:     true,
		NoRunID:             true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows, err := sf.QueryContext(ctx, fmt.Sprintf(`
		SELECT ID, TYPEOF(DATA), TYPEOF(TAGS), DATA IS NULL, DATA:"a"::INT, DATA:"s"::STRING, ARRAY_SIZE(TAGS),
			AMOUNT::STRING, ACTIVE,
			TO_VARCHAR(CONVERT_TIMEZONE('UTC', HAPPENED_AT), 'YYYY-MM-DD HH24:MI:SS'),
			TO_VARCHAR(RECORDED_AT, 'YYYY-MM-DD HH24:MI:SS'),
			DAY::STRING, HEX_ENCODE(RAW), NOTE, TYPEOF(LABELS), ARRAY_SIZE(LABELS)
		FROM %s ORDER BY ID`, destTable))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type row struct {
		id                                        int
		dataType, tagsType                        sql.NullString
		dataIsNull                                bool
		a                                         sql.NullInt64
		s                                         sql.NullString
		tagsLen                                   sql.NullInt64
		amount                                    sql.NullString
		active                                    sql.NullBool
		happenedAt, recordedAt, day, rawHex, note sql.NullString
		labelsType                                sql.NullString
		labelsLen                                 sql.NullInt64
	}
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.id, &r.dataType, &r.tagsType, &r.dataIsNull, &r.a, &r.s, &r.tagsLen,
			&r.amount, &r.active, &r.happenedAt, &r.recordedAt, &r.day, &r.rawHex, &r.note, &r.labelsType, &r.labelsLen))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 4)

	assert.Equal(t, "OBJECT", got[0].dataType.String)
	assert.Equal(t, "ARRAY", got[0].tagsType.String)
	assert.Equal(t, int64(1), got[0].a.Int64)
	assert.Equal(t, int64(2), got[0].tagsLen.Int64)
	assert.Equal(t, "12.34", got[0].amount.String)
	assert.True(t, got[0].active.Bool)
	assert.Equal(t, "2026-01-02 03:04:05", got[0].happenedAt.String)
	assert.Equal(t, "2026-01-02 03:04:05", got[0].recordedAt.String)
	assert.Equal(t, "2026-01-02", got[0].day.String)
	assert.Equal(t, "DEADBEEF", got[0].rawHex.String)
	assert.Equal(t, "one", got[0].note.String)
	assert.Equal(t, "ARRAY", got[0].labelsType.String)
	assert.Equal(t, int64(2), got[0].labelsLen.Int64)

	assert.Equal(t, "OBJECT", got[1].dataType.String)
	assert.Equal(t, "it's", got[1].s.String)
	assert.Equal(t, int64(0), got[1].tagsLen.Int64)
	assert.False(t, got[1].amount.Valid)
	assert.Equal(t, int64(0), got[1].labelsLen.Int64)

	assert.True(t, got[2].dataIsNull, "SQL NULL must stay SQL NULL")
	assert.False(t, got[2].dataType.Valid)
	assert.False(t, got[2].tagsType.Valid)
	assert.Equal(t, "0.00", got[2].amount.String)
	assert.False(t, got[2].labelsType.Valid)

	assert.Equal(t, "NULL_VALUE", got[3].dataType.String, "JSON null stays a JSON null")
	assert.False(t, got[3].dataIsNull)
	assert.Equal(t, "VARCHAR", got[3].tagsType.String, "a JSON string scalar is still a string")
}

func TestSnowflakeMergeLoadsColumnOverrideJSONAsParsedVariant(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	pg, err := sql.Open("pgx", sourceURI)
	require.NoError(t, err)
	defer func() { _ = pg.Close() }()

	srcTable := sourceSchema + ".events"
	_, err = pg.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (id INTEGER PRIMARY KEY, payload TEXT)`, srcTable))
	require.NoError(t, err)
	_, err = pg.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, '{"k":"v1"}'), (2, NULL), (3, '[1,2,3]')`, srcTable))
	require.NoError(t, err)

	destTable := "PUBLIC.JSON_MERGE_" + uniqueSuffix()
	sf, err := snowflakeOpenDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+destTable)
		_ = sf.Close()
	})

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         srcTable,
		DestURI:             snowflakeTestURI,
		DestTable:           destTable,
		IncrementalStrategy: "merge",
		PrimaryKeys:         []string{"id"},
		Columns:             "payload:json",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	_, err = pg.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET payload = '{"k":"v2"}' WHERE id = 1`, srcTable))
	require.NoError(t, err)
	_, err = pg.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (4, '{"k":"v4"}')`, srcTable))
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows, err := sf.QueryContext(ctx, fmt.Sprintf(
		`SELECT ID, TYPEOF(PAYLOAD), PAYLOAD IS NULL, PAYLOAD:"k"::STRING FROM %s ORDER BY ID`, destTable))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type row struct {
		id     int
		typeOf sql.NullString
		isNull bool
		k      sql.NullString
	}
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.id, &r.typeOf, &r.isNull, &r.k))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 4)

	assert.Equal(t, "OBJECT", got[0].typeOf.String)
	assert.Equal(t, "v2", got[0].k.String, "merge must update the parsed document")
	assert.True(t, got[1].isNull, "SQL NULL must stay SQL NULL")
	assert.Equal(t, "ARRAY", got[2].typeOf.String)
	assert.Equal(t, "OBJECT", got[3].typeOf.String)
	assert.Equal(t, "v4", got[3].k.String)
}
