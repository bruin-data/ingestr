//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Postgres JSONB under REPLICA IDENTITY DEFAULT: an UPDATE that touches only a
// non-TOASTed column omits the TOASTed payload from the WAL tuple, so the merge
// into Snowflake VARIANT must keep the existing document.
func TestPostgresCDC_SnowflakeMergePreservesUnchangedJSONB(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := context.Background()
	suffix := uniqueSuffix()
	pubName := "test_toast_sf_pub_" + strings.ToLower(suffix)
	srcTable := "public.test_toast_sf_" + strings.ToLower(suffix)
	destTable := "PUBLIC.TEST_TOAST_SF_" + suffix

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	items := make([]int, 200)
	for i := range items {
		items[i] = i
	}
	payload, err := json.Marshal(map[string]interface{}{
		"items":   items,
		"padding": strings.Repeat("x", 8*1024),
	})
	require.NoError(t, err)

	_, err = sourcePool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			payload JSONB NOT NULL,
			status TEXT NOT NULL
		)
	`, srcTable))
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, fmt.Sprintf(`CREATE PUBLICATION %s FOR TABLE %s`, pubName, srcTable))
	require.NoError(t, err)
	_, err = sourcePool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)
	_, err = sourcePool.Exec(
		ctx,
		fmt.Sprintf(`INSERT INTO %s (payload, status) VALUES ($1::jsonb, 'pending')`, srcTable),
		string(payload),
	)
	require.NoError(t, err)

	db, err := snowflakeOpenDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+destTable)
		_ = db.Close()
	})

	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&publication=" + pubName + "&mode=batch",
		DestURI:             snowflakeTestURI,
		SourceTable:         srcTable,
		DestTable:           destTable,
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	_, err = sourcePool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = 'completed' WHERE id = 1`, srcTable))
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var sourcePayload string
	var sourceLen int
	require.NoError(t, sourcePool.QueryRow(ctx, fmt.Sprintf(`
		SELECT payload::text, jsonb_array_length(payload->'items')
		FROM %s WHERE id = 1
	`, srcTable)).Scan(&sourcePayload, &sourceLen))

	var destLen int
	var destStatus string
	var matches bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT ARRAY_SIZE(PAYLOAD:"items"), STATUS, EQUAL_NULL(PARSE_JSON('%s'), PAYLOAD)
		FROM %s WHERE ID = 1
	`, escapeSnowflakeLiteral(sourcePayload), destTable)).Scan(&destLen, &destStatus, &matches))
	assert.Equal(t, sourceLen, destLen, "items array length should match")
	assert.True(t, matches, "unchanged JSONB payload should survive the partial-update merge")
	assert.Equal(t, "completed", destStatus)
}
