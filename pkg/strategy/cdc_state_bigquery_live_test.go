//go:build bigquerylive

package strategy

import (
	"context"
	"os"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	bqdest "github.com/bruin-data/ingestr/pkg/destination/bigquery"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

// Live regression test for the _cdc_lsn widening upgrade path against a real
// BigQuery project. Run with:
//
//	BIGQUERY_LIVE_URI=bigquery://<project>/<dataset> go test -tags bigquerylive -run TestBigQueryCDCStatePositionMigrationLive ./pkg/strategy/
func TestBigQueryCDCStatePositionMigrationLive(t *testing.T) {
	uri := os.Getenv("BIGQUERY_LIVE_URI")
	if uri == "" {
		t.Skip("BIGQUERY_LIVE_URI not set")
	}
	ctx := context.Background()
	dest := bqdest.NewBigQueryDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	defer func() { _ = dest.Close(ctx) }()

	manager, err := NewCDCStateManager(dest, "livetest-connector", "ignored", "")
	require.NoError(t, err)
	stateTable := manager.stateTable
	t.Logf("state table: %s", stateTable)

	_ = dest.DropTable(ctx, stateTable)
	_ = dest.DropTable(ctx, manager.targetTable)
	defer func() {
		_ = dest.DropTable(ctx, stateTable)
		_ = dest.DropTable(ctx, manager.targetTable)
	}()

	// Create the state table exactly as a pre-widening release did: bounded
	// _cdc_lsn STRING(64).
	legacy := &schema.TableSchema{Columns: make([]schema.Column, len(cdcStateSchema.Columns))}
	copy(legacy.Columns, cdcStateSchema.Columns)
	for i := range legacy.Columns {
		if legacy.Columns[i].Name == destination.CDCLSNColumn {
			legacy.Columns[i].MaxLength = 64
		}
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: stateTable, Schema: legacy, PrimaryKeys: []string{"connector_id", "event_id"},
	}))

	// Sanity: without migration, the new unbounded schema must fail reconcile.
	err = dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: stateTable, Schema: cdcStateSchema, PrimaryKeys: []string{"connector_id", "event_id"},
	})
	require.ErrorContains(t, err, "bounded")
	t.Logf("un-migrated reconcile correctly rejected: %v", err)

	// The manager's prepare must widen through the migrator and succeed.
	require.NoError(t, manager.prepareTable(ctx))

	// The column is now unbounded: strict reconcile passes.
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: stateTable, Schema: cdcStateSchema, PrimaryKeys: []string{"connector_id", "event_id"},
	}))

	// No-op path: a fresh manager prepares without needing DDL.
	manager2, err := NewCDCStateManager(dest, "livetest-connector", "ignored", "")
	require.NoError(t, err)
	require.NoError(t, manager2.prepareTable(ctx))
}
