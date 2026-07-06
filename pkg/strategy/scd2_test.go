package strategy

import (
	"context"
	"testing"
	"time"

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

	dest := &fakeDestination{}

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

	// Should have called PrepareTable twice (for target and staging)
	assert.GreaterOrEqual(t, len(dest.prepareCalls), 1)

	// Should have called WriteParallel for staging
	assert.GreaterOrEqual(t, len(dest.writeCalls), 1)
	assert.True(t, dest.writeCalls[0].StagingTable)
	assert.Equal(t, 777, dest.writeCalls[0].LoaderFileSize)

	// Should have called SCD2Table
	assert.Contains(t, dest.calls, "SCD2Table")

	// Should have called DropTable for staging cleanup
	assert.Contains(t, dest.calls, "DropTable")
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
