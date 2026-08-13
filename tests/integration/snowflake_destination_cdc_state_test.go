//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	snowflakedest "github.com/bruin-data/ingestr/pkg/destination/snowflake"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeDestinationManagedCDCState(t *testing.T) {
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := t.Context()
	suffix := uniqueSuffix()
	stateTable := fmt.Sprintf("_BRUIN_STAGING.CDC_STATE_%s", suffix)
	claimTable := fmt.Sprintf("_BRUIN_STAGING.CDC_TARGETS_%s", suffix)
	targetTable := fmt.Sprintf("PUBLIC.CDC_MANAGED_TARGET_%s", suffix)

	dest := snowflakedest.NewSnowflakeDestination()
	require.NoError(t, dest.Connect(ctx, snowflakeTestURI))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })

	db, err := snowflakeOpenDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+stateTable)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+claimTable)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+targetTable)
	})

	cdcStateSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "event_id", DataType: schema.TypeString, MaxLength: 128, Nullable: false},
		{Name: "state_version", DataType: schema.TypeString, MaxLength: 16, Nullable: false},
		{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
		{Name: "source_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
		{Name: "destination_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
		{Name: "state_kind", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
		{Name: "state_generation", DataType: schema.TypeInt64, Nullable: false},
		{Name: "state_status", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
		{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}}
	cdcTargetSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "destination_table", DataType: schema.TypeString, MaxLength: 2048, Nullable: false},
		{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
		{Name: "claimed_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       stateTable,
		Schema:      cdcStateSchema,
		PrimaryKeys: []string{"connector_id", "event_id"},
		DropFirst:   true,
	}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       claimTable,
		Schema:      cdcTargetSchema,
		PrimaryKeys: []string{"destination_table"},
		DropFirst:   true,
	}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: targetTable,
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "payload", DataType: schema.TypeString, Nullable: true},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestamp, Nullable: true},
		}},
		PrimaryKeys: []string{"id"},
		DropFirst:   true,
	}))

	claimA := destination.CDCTargetClaim{
		DestinationTable: targetTable,
		ConnectorID:      "connector-a",
		SourceTable:      "public.users",
	}
	require.NoError(t, dest.ClaimCDCTarget(ctx, claimTable, claimA))
	require.NoError(t, dest.ClaimCDCTarget(ctx, claimTable, claimA), "idempotent reclaim by same owner")

	claimB := destination.CDCTargetClaim{
		DestinationTable: targetTable,
		ConnectorID:      "connector-b",
		SourceTable:      "public.users",
	}
	err = dest.ClaimCDCTarget(ctx, claimTable, claimB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already claimed")

	canonicalMixed, err := dest.CanonicalCDCTarget(ctx, "public.cdc_managed_target_"+suffix)
	require.NoError(t, err)
	canonicalUpper, err := dest.CanonicalCDCTarget(ctx, targetTable)
	require.NoError(t, err)
	require.Equal(t, canonicalUpper, canonicalMixed)

	firstIncarnation, exists, err := dest.CDCTargetIncarnation(ctx, targetTable)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEmpty(t, firstIncarnation)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			"EVENT_ID", "STATE_VERSION", "CONNECTOR_ID", "SOURCE_TABLE", "DESTINATION_TABLE",
			"STATE_KIND", "STATE_GENERATION", "STATE_STATUS", "_CDC_LSN", "RECORDED_AT"
		) VALUES
			('run-1', '2', 'connector-a', '', '', 'run', 1, 'complete', '', CURRENT_TIMESTAMP()),
			('snap-1', '2', 'connector-a', 'public.users', '%s', 'snapshot', 1, 'complete', '00000000/00000010', CURRENT_TIMESTAMP()),
			('run-2', '2', 'connector-a', '', '', 'run', 2, 'complete', '', CURRENT_TIMESTAMP()),
			('ckpt-2', '2', 'connector-a', 'public.users', '%s', 'checkpoint', 2, 'complete', '00000000/00000020', CURRENT_TIMESTAMP()),
			('run-other', '2', 'connector-b', '', '', 'run', 9, 'complete', '', CURRENT_TIMESTAMP())
	`, stateTable, escapeSnowflakeLiteral(canonicalUpper), escapeSnowflakeLiteral(canonicalUpper))))

	entries, err := dest.LoadCDCState(ctx, stateTable, "connector-a")
	require.NoError(t, err)
	require.Len(t, entries, 4)

	fence, err := dest.LoadCDCStateFence(ctx, stateTable, "connector-a")
	require.NoError(t, err)
	require.Equal(t, int64(2), fence.Generation)
	require.Equal(t, []string{"run-2"}, fence.RunEventIDs)

	require.NoError(t, dest.DeleteCDCStateEvents(ctx, stateTable, "connector-a", []string{"snap-1", "ckpt-2"}))
	entries, err = dest.LoadCDCState(ctx, stateTable, "connector-a")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	otherEntries, err := dest.LoadCDCState(ctx, stateTable, "connector-b")
	require.NoError(t, err)
	require.Len(t, otherEntries, 1)

	require.NoError(t, dest.Exec(ctx, "DROP TABLE IF EXISTS "+targetTable))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: targetTable,
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "payload", DataType: schema.TypeString, Nullable: true},
		}},
		PrimaryKeys: []string{"id"},
		DropFirst:   false,
	}))
	recreated, exists, err := dest.CDCTargetIncarnation(ctx, targetTable)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEqual(t, firstIncarnation, recreated, "drop/recreate must change incarnation")

	missing, exists, err := dest.CDCTargetIncarnation(ctx, "PUBLIC.CDC_MANAGED_MISSING_"+suffix)
	require.NoError(t, err)
	require.False(t, exists)
	require.Empty(t, missing)
}

func escapeSnowflakeLiteral(v string) string {
	return strings.ReplaceAll(v, `'`, `''`)
}
