package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keylessCDCSchema mimics what the Postgres CDC source emits for a table with
// no primary key and no replica identity index: source columns plus the CDC
// metadata columns, and an empty PrimaryKeys list.
func keylessCDCSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "payload", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
	}
}

func schemaHasColumn(s *schema.TableSchema, name string) bool {
	for _, col := range s.Columns {
		if col.Name == name {
			return true
		}
	}
	return false
}

func TestMergeStrategy_MultiTableKeylessCDCLandsAppendOnly(t *testing.T) {
	dest := &fakeDestination{}
	table := source.SourceTableInfo{Name: "public.events", Schema: keylessCDCSchema()}

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.events"}
	close(records)

	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{"public.events": "events"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))

	dest.mu.Lock()
	defer dest.mu.Unlock()

	// One PrepareTable call: the final table, keyless, CDC-relaxed, and keeping
	// the staging-only _cdc_unchanged_cols column since batches carry it.
	require.Len(t, dest.prepareCalls, 1)
	prep := dest.prepareCalls[0]
	assert.Equal(t, "events", prep.Table)
	assert.Empty(t, prep.PrimaryKeys)
	assert.True(t, prep.CDCMode)
	assert.False(t, prep.DropFirst)
	assert.True(t, schemaHasColumn(prep.Schema, destination.CDCUnchangedColsColumn))

	// Rows land in the final table directly; nothing is merged or dropped.
	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, "events", dest.writeCalls[0].Table)
	assert.Empty(t, dest.mergeCalls)
	assert.Empty(t, dest.dropCalls)
}

func TestMergeStrategy_MultiTableMixedKeyedAndKeyless(t *testing.T) {
	dest := &fakeDestination{}
	keyed := source.SourceTableInfo{Name: "public.users", Schema: streamTestSchema(), PrimaryKeys: []string{"id"}}
	keyless := source.SourceTableInfo{Name: "public.events", Schema: keylessCDCSchema()}

	records := make(chan source.RecordBatchResult)
	close(records)

	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{keyed, keyless}, records: records}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{"public.users": "users", "public.events": "events"},
	}

	require.NoError(t, (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job))

	dest.mu.Lock()
	defer dest.mu.Unlock()

	// The keyed table merges via staging; the keyless one never reaches MergeTable.
	require.Len(t, dest.mergeCalls, 1)
	assert.Equal(t, "users", dest.mergeCalls[0].TargetTable)
}

func TestStreaming_PrepareTableKeylessCDCSkipsStaging(t *testing.T) {
	dest := &fakeDestination{}
	exec := NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	})

	st := &streamTableState{
		destTable: "events",
		schema:    keylessCDCSchema(),
		isCDC:     true,
	}
	require.NoError(t, exec.prepareTable(context.Background(), dest, &config.IngestConfig{}, st))

	assert.Empty(t, st.stagingTable)

	dest.mu.Lock()
	require.Len(t, dest.prepareCalls, 1)
	prep := dest.prepareCalls[0]
	dest.mu.Unlock()
	assert.Equal(t, "events", prep.Table)
	assert.True(t, prep.CDCMode)
	assert.True(t, schemaHasColumn(prep.Schema, destination.CDCUnchangedColsColumn))

	// A flush for this table writes directly to the destination table and
	// performs no merge.
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.events"}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, "events", dest.writeCalls[0].Table)
	assert.False(t, dest.writeCalls[0].StagingTable)
	assert.Empty(t, dest.mergeCalls)
}
