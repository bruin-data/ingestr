//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMSSQLToMSSQL_TimeColumn_CustomQueryReplace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	suffix := uniqueSuffix()
	sourceTable := fmt.Sprintf("dbo.time_repro_src_%s", suffix)
	destTable := fmt.Sprintf("dbo.time_repro_dst_%s", suffix)

	dropMSSQLTable(t, ctx, db, sourceTable)
	t.Cleanup(func() {
		dropMSSQLTable(t, ctx, db, sourceTable)
		dropMSSQLTable(t, ctx, db, destTable)
	})

	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id BIGINT NOT NULL,
		start_date DATE NULL,
		start_time TIME NULL,
		end_time TIME(6) NULL,
		created_at DATETIME NULL
	)`, quoteTableMSSQL(sourceTable)))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, start_date, start_time, end_time, created_at) VALUES
		(1, '2024-03-01', '09:15:30.1234567', '17:45:00.654321', '2024-03-01T10:00:00'),
		(2, '2024-03-02', '00:00:00', '23:59:59.999999', '2024-03-02T11:30:00'),
		(3, NULL, NULL, NULL, NULL)`, quoteTableMSSQL(sourceTable)))
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI:           mssqlDest.uri,
		SourceTable:         fmt.Sprintf("query:select * from %s", quoteTableMSSQL(sourceTable)),
		DestURI:             mssqlDest.uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableMSSQL(destTable))).Scan(&count))
	assert.Equal(t, 3, count)

	var startTime, endTime sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT CONVERT(VARCHAR(16), start_time, 121), CONVERT(VARCHAR(16), end_time, 121) FROM %s WHERE id = 1",
		quoteTableMSSQL(destTable),
	)).Scan(&startTime, &endTime))
	assert.Equal(t, "09:15:30.123456", startTime.String, "TIME value should round-trip with microsecond precision")
	assert.Equal(t, "17:45:00.654321", endTime.String)

	var nullTime sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT CONVERT(VARCHAR(16), start_time, 121) FROM %s WHERE id = 3",
		quoteTableMSSQL(destTable),
	)).Scan(&nullTime))
	assert.False(t, nullTime.Valid, "NULL TIME should stay NULL")
}
