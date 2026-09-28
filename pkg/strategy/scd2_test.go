package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSCD2Strategy_Validate_RequiresPrimaryKeys(t *testing.T) {
	strategy := &SCD2Strategy{}

	// Without primary keys should fail
	cfg := &config.IngestConfig{
		SourceTable: "src",
		DestTable:   "dst",
		PrimaryKeys: nil,
	}
	err := strategy.Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary_key")

	// With primary keys should pass
	cfg.PrimaryKeys = []string{"id"}
	err = strategy.Validate(cfg)
	require.NoError(t, err)
}

func TestSCD2Strategy_Validate_RequiresIncrementalKeyWithExtractPartitioning(t *testing.T) {
	strategy := &SCD2Strategy{}
	cfg := &config.IngestConfig{
		PrimaryKeys:              []string{"id"},
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 24 * time.Hour,
		IncrementalStrategy:      config.StrategySCD2,
	}

	err := strategy.Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incremental_key")

	cfg.IncrementalKey = "updated_at"
	require.NoError(t, strategy.Validate(cfg))
}

func TestSCD2Strategy_Execute_BasicFlow(t *testing.T) {
	strategy := &SCD2Strategy{}

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	src := &fakeSourceTable{
		name:           "src_table",
		primaryKeys:    []string{"id"},
		strategy:       config.StrategySCD2,
		hasKnownSchema: true,
		tableSchema:    tableSchema,
		readCh: mustClosedRecords(
			source.RecordBatchResult{Batch: intStringRecordBatch(t, "id", []int64{1, 2}, "name", []string{"alice", "bob"})},
		),
	}

	var validFrom []int64
	dest := &fakeDestination{observeBatch: func(batch arrow.RecordBatch) {
		require.Equal(t, int64(5), batch.NumCols())
		require.Equal(t, []int64{1, 2}, batch.Column(0).(*array.Int64).Int64Values())
		for row := 0; row < int(batch.NumRows()); row++ {
			validFrom = append(validFrom, int64(batch.Column(2).(*array.Timestamp).Value(row)))
			require.True(t, batch.Column(3).IsNull(row))
			require.True(t, batch.Column(4).(*array.Boolean).Value(row))
		}
	}}

	job := &IngestionJob{
		Config: &config.IngestConfig{
			SourceTable:         "src_table",
			DestTable:           "ds.tbl",
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategySCD2,
			LoaderFileSize:      777,
		},
		Table:       src,
		Destination: dest,
		Schema:      tableSchema,
	}

	err := strategy.Execute(context.Background(), job)
	require.NoError(t, err)

	require.Len(t, dest.prepareCalls, 2)
	target, staging := dest.prepareCalls[0], dest.prepareCalls[1]
	assert.Equal(t, "ds.tbl", target.Table)
	assert.False(t, target.DropFirst)
	assert.Empty(t, target.PrimaryKeys)
	assert.NotEqual(t, target.Table, staging.Table)
	assert.True(t, staging.DropFirst)
	assert.Empty(t, staging.PrimaryKeys)
	assert.Equal(t, []string{"id", "name", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"}, target.Schema.ColumnNames())
	assert.Equal(t, target.Schema, staging.Schema)

	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, staging.Table, dest.writeCalls[0].Table)
	assert.Equal(t, staging.Schema, dest.writeCalls[0].Schema)
	assert.True(t, dest.writeCalls[0].StagingTable)
	assert.Equal(t, 777, dest.writeCalls[0].LoaderFileSize)

	require.Len(t, dest.scd2Calls, 1)
	merge := dest.scd2Calls[0]
	assert.Equal(t, staging.Table, merge.StagingTable)
	assert.Equal(t, target.Table, merge.TargetTable)
	assert.Equal(t, []string{"id"}, merge.PrimaryKeys)
	assert.Equal(t, []string{"id", "name"}, merge.Columns)
	assert.Equal(t, tableSchema, merge.Schema)
	assert.False(t, merge.Timestamp.IsZero())
	assert.Equal(t, []int64{merge.Timestamp.UnixMicro(), merge.Timestamp.UnixMicro()}, validFrom)
	assert.Equal(t, []string{staging.Table}, dest.dropCalls)
	assert.Equal(t, []string{"PrepareTable", "PrepareTable", "WriteParallel", "SCD2Table", "DropTable"}, dest.calls)
}

func TestSCD2Strategy_RejectsCDCBeforeDestinationOrSourceWork(t *testing.T) {
	job, src, dest := minimalJob()
	job.Schema = keylessCDCSchema()
	job.Config.IncrementalStrategy = config.StrategySCD2

	err := (&SCD2Strategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "not supported for CDC records")
	require.Empty(t, dest.prepareCalls)
	require.Empty(t, dest.writeCalls)
	require.False(t, src.readCalled)
}

func TestSCD2Strategy_ExtendSchemaWithSCDColumns(t *testing.T) {
	original := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	extended := extendSchemaWithSCDColumns(original)

	// Should have 3 additional columns
	assert.Equal(t, 5, len(extended.Columns))
	assert.Equal(t, []string{"id"}, extended.PrimaryKeys)

	// Check SCD columns exist
	colNames := make([]string, len(extended.Columns))
	for i, col := range extended.Columns {
		colNames[i] = col.Name
	}

	assert.Contains(t, colNames, "_scd_valid_from")
	assert.Contains(t, colNames, "_scd_valid_to")
	assert.Contains(t, colNames, "_scd_is_current")

	// Check SCD column types
	for _, col := range extended.Columns {
		switch col.Name {
		case "_scd_valid_from":
			assert.Equal(t, schema.TypeTimestampTZ, col.DataType)
			assert.False(t, col.Nullable)
		case "_scd_valid_to":
			assert.Equal(t, schema.TypeTimestampTZ, col.DataType)
			assert.True(t, col.Nullable)
		case "_scd_is_current":
			assert.Equal(t, schema.TypeBoolean, col.DataType)
			assert.False(t, col.Nullable)
		}
	}
}

func TestSCD2Strategy_ExtendSchemaWithSCDColumns_CaseInsensitive(t *testing.T) {
	original := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "_SCD_VALID_FROM", DataType: schema.TypeTimestampTZ, Nullable: false},
			{Name: "_SCD_VALID_TO", DataType: schema.TypeTimestampTZ, Nullable: true},
			{Name: "_SCD_IS_CURRENT", DataType: schema.TypeBoolean, Nullable: false},
		},
		PrimaryKeys: []string{"id"},
	}

	extended := extendSchemaWithSCDColumns(original)

	assert.Equal(t, 5, len(extended.Columns))

	colNames := make([]string, len(extended.Columns))
	for i, col := range extended.Columns {
		colNames[i] = col.Name
	}
	assert.Equal(t, []string{"id", "name", "_SCD_VALID_FROM", "_SCD_VALID_TO", "_SCD_IS_CURRENT"}, colNames)
}
