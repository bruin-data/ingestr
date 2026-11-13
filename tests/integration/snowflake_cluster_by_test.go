//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeDestinationAppliesClusterBy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := context.Background()
	sf, err := snowflakeOpenDB()
	require.NoError(t, err)
	suffix := uniqueSuffix()
	srcTable := "PUBLIC.SRC_CLUSTER_" + suffix
	t.Cleanup(func() { _ = sf.Close() })
	dropAfter := func(table string) {
		t.Cleanup(func() { _, _ = sf.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })
	}
	dropAfter(srcTable)

	_, err = sf.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (ID INT, EVENT_DATE DATE, NAME VARCHAR)`, srcTable))
	require.NoError(t, err)
	_, err = sf.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, '2026-01-01', 'a'), (2, '2026-01-02', 'b')`, srcTable))
	require.NoError(t, err)

	for _, strategy := range []config.IncrementalStrategy{config.StrategyReplace, config.StrategyMerge} {
		t.Run(string(strategy), func(t *testing.T) {
			destName := strings.ToUpper("CLUSTER_" + string(strategy) + "_" + suffix)
			dropAfter("PUBLIC." + destName)
			cfg := &config.IngestConfig{
				SourceURI:           snowflakeTestURI,
				SourceTable:         srcTable,
				DestURI:             snowflakeTestURI,
				DestTable:           "PUBLIC." + destName,
				IncrementalStrategy: strategy,
				PrimaryKeys:         []string{"id"},
				ClusterBy:           []string{"event_date", "id", "not_a_column"},
				StagingDataset:      "PUBLIC",
				NoLoadTimestamp:     true,
				NoRunID:             true,
				Yes:                 true,
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			var clusteringKey string
			require.NoError(t, sf.QueryRowContext(ctx,
				"SELECT CLUSTERING_KEY FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = 'PUBLIC' AND TABLE_NAME = ?", destName,
			).Scan(&clusteringKey))
			assert.Equal(t, `LINEAR("EVENT_DATE", "ID")`, clusteringKey)
		})
	}
}
